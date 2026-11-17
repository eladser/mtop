// Package demo is a scripted stand-in for the servers and the GPU, so
// `mtop --demo` has something to show without any of them. It touches
// no network, files or processes: rows and GPU stats come from a pure
// function of time, and traffic goes straight into a proxy.Store.
package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
)

// Loop is how long the scenario runs before it starts over.
const Loop = 40 * time.Second

const (
	sagFrom, sagTo = 16 * time.Second, 26 * time.Second
	sagCut         = 0.35
	gpuName        = "Simulated GPU (24 GB)"
	gpuMiB         = 24576
	llamaModel     = "gpt-oss-20b"
)

type model struct {
	sources.Row
	ttl time.Duration
}

var models = []model{
	{sources.Row{Name: "qwen3:0.6b", Size: "0.6b", Quant: "Q4_K_M", VRAM: 1 << 30, Ctx: 8192}, 25 * time.Minute},
	{sources.Row{Name: "llama3.1:8b", Size: "8b", Quant: "Q4_K_M", VRAM: 6120328396, Ctx: 8192}, 4*time.Minute + 40*time.Second},
	{sources.Row{Name: "qwen3:32b", Size: "32b", Quant: "Q4_K_M", VRAM: 8 << 30, CPU: 38, Ctx: 4096}, 4*time.Minute + 12*time.Second},
	{sources.Row{Name: llamaModel, Size: "32k ctx", Quant: "MXFP4", Ctx: 32768, Slots: 2, Alias: llamaModel}, 0},
}

// one scripted request; text is prompt, thinking, completion for the inspector
type req struct {
	model        string
	path         string
	at, ttft     time.Duration
	rate         float64 // tok/s once decoding
	toks, prompt int
	sags         bool // slows down while the GPU is throttling
	text         [3]string
}

const ollamaChat, openaiChat = "/api/chat", "/v1/chat/completions"

var script = []req{
	{model: "llama3.1:8b", path: ollamaChat, at: 1000 * time.Millisecond, ttft: 400 * time.Millisecond, rate: 62, toks: 220, prompt: 310, sags: true,
		text: [3]string{"Summarize the main differences between TCP and UDP for a junior engineer.", "", "TCP sets up a connection, numbers every byte and retransmits what gets lost. UDP just sends datagrams and moves on. Use TCP when order and delivery matter, UDP when latency does: voice, games, DNS lookups."}},
	{model: "qwen3:32b", path: ollamaChat, at: 3 * time.Second, ttft: 2100 * time.Millisecond, rate: 9, toks: 60, prompt: 420,
		text: [3]string{"Write a regex that matches ISO 8601 dates like 2026-11-17.", "Four digit year, dash, two digit month, dash, two digit day. Validating the month and day ranges is a bonus.", `\b\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])\b`}},
	{model: "qwen3:32b", path: ollamaChat, at: 4600 * time.Millisecond, ttft: 2300 * time.Millisecond, rate: 9, toks: 10, prompt: 3980,
		text: [3]string{"(a pasted log file, 3980 tokens)\n\nWhat is the first error in this log?", "", "The first error is at 03:14:07, a refused connection to the cache host."}},
	{model: "qwen3:0.6b", path: ollamaChat, at: 10 * time.Second, ttft: 150 * time.Millisecond, rate: 95, toks: 45, prompt: 60,
		text: [3]string{"Give this commit a one-line message: renamed the retry flag", "", "rename retry flag to max-retries"}},
	{model: "llama3.1:8b", path: ollamaChat, at: 15500 * time.Millisecond, ttft: 400 * time.Millisecond, rate: 62, toks: 400, prompt: 540, sags: true,
		text: [3]string{"Explain how a B-tree stays balanced when you insert a key into a full node.", "", "A full node splits around its median key: the median moves up into the parent and the two halves become siblings. If the parent was full too, the split repeats upward, and the tree only grows taller when the root itself splits."}},
	{model: "qwen3:0.6b", path: ollamaChat, at: 21 * time.Second, ttft: 150 * time.Millisecond, rate: 95, toks: 50, prompt: 55,
		text: [3]string{"Translate to French: the build is green again.", "", "Le build est de nouveau au vert."}},
	// four on two slots: the last two wait for a slot, hence the long ttft
	{model: llamaModel, path: openaiChat, at: 28 * time.Second, ttft: 500 * time.Millisecond, rate: 40, toks: 200, prompt: 900,
		text: [3]string{"Draft release notes for a patch that fixes a crash on empty config files.", "Short notes, one bullet per change, no marketing.", "- Fixed a crash when the config file exists but is empty.\n- An empty file now behaves like a missing one."}},
	{model: llamaModel, path: openaiChat, at: 28400 * time.Millisecond, ttft: 500 * time.Millisecond, rate: 40, toks: 200, prompt: 880,
		text: [3]string{"List three ways to cut the cold-start time of a Go CLI.", "", "1. Avoid init() work.\n2. Lazy-load big tables.\n3. Skip reflection-heavy libraries."}},
	{model: llamaModel, path: openaiChat, at: 28800 * time.Millisecond, ttft: 4700 * time.Millisecond, rate: 40, toks: 200, prompt: 910,
		text: [3]string{"Write a SQL query for the ten most recent orders per customer.", "", "SELECT * FROM (SELECT o.*, ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY created_at DESC) rn FROM orders o) t WHERE rn <= 10;"}},
	{model: llamaModel, path: openaiChat, at: 29200 * time.Millisecond, ttft: 4700 * time.Millisecond, rate: 40, toks: 200, prompt: 870,
		text: [3]string{"Shorten this sentence: we are in the process of migrating.", "", "We're migrating."}},
}

var ends = func() []time.Duration {
	out := make([]time.Duration, len(script))
	for i, r := range script {
		out[i] = r.end()
	}
	return out
}()

func (r req) decodeStart() time.Duration { return r.at + r.ttft }

// tokensAt is how many tokens the request has produced by loop offset e.
func (r req) tokensAt(e time.Duration) int {
	d0 := r.decodeStart()
	if e <= d0 {
		return 0
	}
	dt := (e - d0).Seconds()
	if r.sags {
		lo, hi := max(d0, sagFrom), min(e, sagTo)
		if hi > lo {
			dt -= sagCut * (hi - lo).Seconds()
		}
	}
	return min(r.toks, int(r.rate*dt))
}

// end is the loop offset where the request finishes.
func (r req) end() time.Duration {
	lo, hi := r.decodeStart(), 3*Loop
	for hi-lo > time.Millisecond {
		mid := lo + (hi-lo)/2
		if r.tokensAt(mid) >= r.toks {
			hi = mid
		} else {
			lo = mid
		}
	}
	return hi
}

func jitter(bucket int64) float64 {
	return rand.New(rand.NewPCG(7, uint64(bucket))).Float64()*2 - 1
}

// state is what the servers and the GPU report e into the loop.
// Same offset, same answer.
func state(e time.Duration) ([]model, gpu.Stats) {
	e %= Loop
	var active, decoding, onLlama int
	for i, r := range script {
		if e >= r.at && e < ends[i] {
			active++
			if r.model == llamaModel {
				onLlama++
			}
			if e >= r.decodeStart() {
				decoding++
			}
		}
	}
	out := make([]model, len(models))
	copy(out, models)
	for i := range out {
		if out[i].Name != llamaModel {
			continue
		}
		out[i].Busy = min(onLlama, out[i].Slots)
		out[i].Queued = onLlama - out[i].Busy
		out[i].Note = fmt.Sprintf("%d running", out[i].Busy)
		if out[i].Queued > 0 {
			out[i].Note += fmt.Sprintf(" · %d queued", out[i].Queued)
		}
		out[i].Note += " · draft 71%"
	}

	j := jitter(int64(e / (500 * time.Millisecond)))
	util := 6 + 45*decoding + 15*(active-decoding) + int(4*j)
	util = max(min(util, 97), 3)

	temp := 62.0
	switch {
	case e < sagFrom:
	case e < sagTo:
		temp += 23 * float64(e-sagFrom) / float64(sagTo-sagFrom)
	case e < sagTo+10*time.Second:
		temp += 23 * (1 - float64(e-sagTo)/float64(10*time.Second))
	}
	g := gpu.Stats{Name: gpuName, Util: util, MemTotal: gpuMiB, Temp: int(temp + 0.5),
		Power: 55 + float64(util)*3.1 + 3*j}
	if e >= sagFrom && e < sagTo {
		g.Throttle = gpu.ThrottleThermal
	}
	return out, g
}

// Sim is the fake Scanner and GPU, and the traffic driver.
type Sim struct {
	mu      sync.Mutex
	now     func() time.Time
	t0      time.Time
	dropped map[string]int64 // model -> loop it was unloaded in

	cycle int64
	fl    []*flight
}

type flight struct {
	live   *proxy.Live
	sent   int
	begun  bool
	closed bool
}

func New() *Sim {
	s := &Sim{now: time.Now, dropped: map[string]int64{}, fl: make([]*flight, len(script))}
	s.t0 = s.now()
	return s
}

func (s *Sim) offset() (time.Duration, int64) {
	t := s.now().Sub(s.t0)
	return t % Loop, int64(t / Loop)
}

func (s *Sim) visible(rows []model, cycle int64) []sources.Row {
	var out []sources.Row
	for _, m := range rows {
		if c, ok := s.dropped[m.Name]; ok && c == cycle {
			continue
		}
		r := m.Row
		r.From, r.Base = "demo", "demo"
		if m.ttl > 0 {
			r.Expires = s.t0.Add(time.Duration(cycle+1)*Loop + m.ttl) // counts down, then a request "refreshes" it
		}
		name := m.Name
		r.Unload = func() error {
			s.mu.Lock()
			defer s.mu.Unlock()
			_, c := s.offset()
			s.dropped[name] = c
			return nil
		}
		out = append(out, r)
	}
	return out
}

func (s *Sim) Scan() ([]sources.Row, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, c := s.offset()
	rows, _ := state(e)
	return s.visible(rows, c), []string{"demo"}, nil
}

func (s *Sim) OnDisk() int { return 7 }

func (s *Sim) Available() bool { return true }

func (s *Sim) Read() ([]gpu.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, c := s.offset()
	_, g := state(e)
	for _, r := range s.visible(models, c) {
		g.MemUsed += int(r.VRAM >> 20)
	}
	g.MemUsed += 1800 + int(80*jitter(int64(e/time.Second)+99)) // context buffers
	return []gpu.Stats{g}, nil
}

// Run feeds the store from the script until ctx ends. Each value on
// step is a cue to catch up to now(); the value itself is ignored.
func (s *Sim) Run(ctx context.Context, store *proxy.Store, now func() time.Time, step <-chan time.Time) {
	s.mu.Lock()
	s.now = now
	s.t0 = now()
	s.cycle = 0
	s.mu.Unlock()
	defer s.drop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-step:
			s.advance(store)
		}
	}
}

// drop ends flights still open, so a stopped sim leaves none behind.
func (s *Sim) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.fl {
		if f != nil && f.begun && !f.closed {
			f.live.End()
			f.closed = true
		}
	}
}

func (s *Sim) advance(store *proxy.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, c := s.offset()
	for ; s.cycle <= c; s.cycle++ {
		at := Loop
		if s.cycle == c {
			at = e
		}
		s.play(store, s.t0.Add(time.Duration(s.cycle)*Loop), at)
		if s.cycle < c {
			s.fl = make([]*flight, len(script))
		}
	}
	s.cycle = c
}

func (s *Sim) play(store *proxy.Store, base time.Time, e time.Duration) {
	for i, r := range script {
		f := s.fl[i]
		if f == nil {
			f = &flight{}
			s.fl[i] = f
		}
		if f.closed || e < r.at {
			continue
		}
		if !f.begun {
			f.live = store.Begin(r.path, r.model, true, base.Add(r.at))
			f.begun = true
		}
		if n := r.tokensAt(e) - f.sent; n > 0 {
			f.live.Tick(n, base.Add(min(e, ends[i])))
			f.sent += n
		}
		if f.sent >= r.toks {
			store.Complete(f.live, finished(r, ends[i], base))
			f.closed = true
		}
	}
}

func finished(r req, end time.Duration, base time.Time) proxy.Request {
	var ctx int
	for _, m := range models {
		if m.Name == r.model {
			ctx = m.Ctx
		}
	}
	return proxy.Request{
		When: base.Add(end), Path: r.path, Model: r.model,
		PromptTk: r.prompt, OutTk: r.toks,
		TokSec: float64(r.toks) / (end - r.decodeStart()).Seconds(),
		Total:  end - r.at, PromptEval: r.ttft, Ctx: ctx,
		Prompt: r.text[0], Thinking: r.text[1], Completion: r.text[2],
	}
}
