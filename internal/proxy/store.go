package proxy

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Request struct {
	When       time.Time
	Path       string
	Model      string
	PromptTk   int
	OutTk      int
	TokSec     float64
	Total      time.Duration
	Load       time.Duration // model load time (ollama, inspector)
	PromptEval time.Duration // prompt processing time (ollama, inspector)
	Prompt     string        // captured only with -inspect
	Completion string        // captured only with -inspect
}

// GPUSample is the slice of GPU state /metrics re-exports. It's a copy
// of what internal/gpu reads, kept here so this package doesn't import
// it just to render some gauges.
type GPUSample struct {
	Name              string
	Util              int
	MemUsed, MemTotal int
	Temp              int
	Power             float64
}

// Store keeps the last N proxied requests, newest first.
type Store struct {
	mu    sync.Mutex
	reqs  []Request
	gpus  []GPUSample
	max   int
	err   error
	onAdd func(Request)
}

func NewStore(max int) *Store {
	return &Store{max: max}
}

func (s *Store) Add(r Request) {
	s.mu.Lock()
	s.reqs = append([]Request{r}, s.reqs...)
	if len(s.reqs) > s.max {
		s.reqs = s.reqs[:s.max]
	}
	fn := s.onAdd
	s.mu.Unlock()
	if fn != nil {
		fn(r)
	}
}

// Preload drops in requests recovered from disk on startup. reqs is
// newest-first like everything else here, and goes behind whatever's
// already live. Doesn't fire onAdd, so loading doesn't re-write history.
func (s *Store) Preload(reqs []Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, reqs...)
	if len(s.reqs) > s.max {
		s.reqs = s.reqs[:s.max]
	}
}

// OnAdd runs fn after each new request lands. Used to append history.
func (s *Store) OnAdd(fn func(Request)) {
	s.mu.Lock()
	s.onAdd = fn
	s.mu.Unlock()
}

// SetGPU stashes the latest GPU read so /metrics can include it.
func (s *Store) SetGPU(g []GPUSample) {
	s.mu.Lock()
	s.gpus = g
	s.mu.Unlock()
}

func (s *Store) Recent(n int) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.reqs) {
		n = len(s.reqs)
	}
	out := make([]Request, n)
	copy(out, s.reqs[:n])
	return out
}

// TokRates returns tok/s of the last n requests, oldest first.
func (s *Store) TokRates(n int) []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.reqs) {
		n = len(s.reqs)
	}
	out := make([]float64, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, s.reqs[i].TokSec)
	}
	return out
}

// LastSeen returns when a model last handled a request through the
// proxy, or zero if it never did.
func (s *Store) LastSeen(model string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reqs {
		if r.Model == model {
			return r.When
		}
	}
	return time.Time{}
}

type ModelStat struct {
	Model    string
	Count    int
	AvgTok   float64
	P50, P95 float64
	OutTk    int
}

func (s *Store) snapshot() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.reqs))
	copy(out, s.reqs)
	return out
}

// ByModel aggregates everything seen so far, busiest model first.
func (s *Store) ByModel() []ModelStat { return byModel(s.snapshot()) }

// Percentiles of tok/s across everything in the buffer.
func (s *Store) Percentiles() (p50, p95 float64) { return percentiles(s.snapshot()) }

func byModel(reqs []Request) []ModelStat {
	rates := map[string][]float64{}
	out := map[string]int{}
	for _, r := range reqs {
		rates[r.Model] = append(rates[r.Model], r.TokSec)
		out[r.Model] += r.OutTk
	}
	var all []ModelStat
	for model, rs := range rates {
		sort.Float64s(rs)
		sum := 0.0
		for _, v := range rs {
			sum += v
		}
		all = append(all, ModelStat{
			Model:  model,
			Count:  len(rs),
			AvgTok: sum / float64(len(rs)),
			P50:    pct(rs, 0.50),
			P95:    pct(rs, 0.95),
			OutTk:  out[model],
		})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Count > all[j].Count })
	return all
}

func percentiles(reqs []Request) (p50, p95 float64) {
	rs := make([]float64, 0, len(reqs))
	for _, r := range reqs {
		rs = append(rs, r.TokSec)
	}
	sort.Float64s(rs)
	return pct(rs, 0.50), pct(rs, 0.95)
}

func pct(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(q*float64(len(sorted)-1))]
}

// PromText renders what the proxy has seen in prometheus exposition
// format, served at /metrics on the proxy port. One snapshot so the
// per-model lines and the percentiles describe the same set of requests.
func (s *Store) PromText() string {
	s.mu.Lock()
	reqs := make([]Request, len(s.reqs))
	copy(reqs, s.reqs)
	gpus := s.gpus
	s.mu.Unlock()

	var b strings.Builder
	for _, m := range byModel(reqs) {
		fmt.Fprintf(&b, "mtop_requests_total{model=%q} %d\n", m.Model, m.Count)
		fmt.Fprintf(&b, "mtop_tokens_out_total{model=%q} %d\n", m.Model, m.OutTk)
		fmt.Fprintf(&b, "mtop_tok_per_s_avg{model=%q} %.1f\n", m.Model, m.AvgTok)
	}
	p50, p95 := percentiles(reqs)
	fmt.Fprintf(&b, "mtop_tok_per_s{quantile=\"0.5\"} %.1f\n", p50)
	fmt.Fprintf(&b, "mtop_tok_per_s{quantile=\"0.95\"} %.1f\n", p95)
	for _, g := range gpus {
		fmt.Fprintf(&b, "mtop_gpu_util{gpu=%q} %d\n", g.Name, g.Util)
		fmt.Fprintf(&b, "mtop_gpu_mem_used_mib{gpu=%q} %d\n", g.Name, g.MemUsed)
		fmt.Fprintf(&b, "mtop_gpu_mem_total_mib{gpu=%q} %d\n", g.Name, g.MemTotal)
		fmt.Fprintf(&b, "mtop_gpu_temp_c{gpu=%q} %d\n", g.Name, g.Temp)
		fmt.Fprintf(&b, "mtop_gpu_power_w{gpu=%q} %.0f\n", g.Name, g.Power)
	}
	return b.String()
}

func (s *Store) SetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
