package demo

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
	"github.com/eladser/mtop/internal/ui"
)

var (
	_ ui.Scanner = (*Sim)(nil)
	_ ui.GPU     = (*Sim)(nil)
)

func sec(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }

func byName(ms []model, name string) model {
	for _, m := range ms {
		if m.Name == name {
			return m
		}
	}
	return model{}
}

func TestScriptFitsTheLoop(t *testing.T) {
	for i, r := range script {
		if ends[i] >= Loop-sec(0.5) {
			t.Errorf("request %d (%s) ends at %v, too close to the wrap", i, r.model, ends[i])
		}
		if r.tokensAt(ends[i]) != r.toks {
			t.Errorf("request %d not done at its end", i)
		}
	}
}

func TestStateRows(t *testing.T) {
	rows, _ := state(0)
	if len(rows) != 4 {
		t.Fatalf("want 4 rows, got %d", len(rows))
	}
	if m := byName(rows, "qwen3:32b"); m.CPU != 38 || m.Ctx != 4096 {
		t.Errorf("32b should be 38%% on cpu with ctx 4096: %+v", m.Row)
	}
	if m := byName(rows, "llama3.1:8b"); m.Ctx != 8192 {
		t.Errorf("8b ctx: %+v", m.Row)
	}
	if m := byName(rows, llamaModel); m.Slots != 2 || m.Note != "0 running · draft 71%" {
		t.Errorf("idle llama.cpp row: slots %d note %q", m.Slots, m.Note)
	}
}

func TestStateQueue(t *testing.T) {
	tests := []struct {
		at           float64
		busy, queued int
		note         string
	}{
		{27, 0, 0, "0 running · draft 71%"},
		{28.5, 2, 0, "2 running · draft 71%"},
		{30, 2, 2, "2 running · 2 queued · draft 71%"},
		{34, 2, 2, "2 running · 2 queued · draft 71%"},
		{36, 2, 0, "2 running · draft 71%"},
		{39.9, 0, 0, "0 running · draft 71%"},
	}
	for _, tt := range tests {
		rows, _ := state(sec(tt.at))
		m := byName(rows, llamaModel)
		if m.Busy != tt.busy || m.Queued != tt.queued || m.Note != tt.note {
			t.Errorf("t=%v: busy %d queued %d note %q", tt.at, m.Busy, m.Queued, m.Note)
		}
	}
}

func TestStateGPU(t *testing.T) {
	_, g := state(0)
	if g.Name != "Simulated GPU (24 GB)" || g.MemTotal != 24576 || g.Temp != 62 || g.Throttle != 0 {
		t.Errorf("t=0: %+v", g)
	}
	for _, at := range []float64{16.5, 20, 25} {
		_, g := state(sec(at))
		if g.Throttle&gpu.ThrottleThermal == 0 {
			t.Errorf("t=%v should be throttling: %+v", at, g)
		}
		if g.Util < 30 {
			t.Errorf("t=%v util %d under the throttle debounce floor", at, g.Util)
		}
	}
	if _, g := state(sec(26)); g.Temp != 85 || g.Throttle != 0 {
		t.Errorf("t=26: want 85C and the flag off, got %+v", g)
	}
	if _, g := state(sec(15)); g.Throttle != 0 {
		t.Errorf("t=15 throttling: %+v", g)
	}
	if a, b := stateTemp(20), stateTemp(20); a != b {
		t.Error("state isn't pure")
	}
}

func stateTemp(at float64) int { _, g := state(sec(at)); return g.Temp }

func TestAlertsNeverTrip(t *testing.T) {
	s := New()
	base := time.Now()
	s.now = func() time.Time { return base }
	for ms := 0; ms < int(2*Loop/time.Millisecond); ms += 100 {
		at := base.Add(time.Duration(ms) * time.Millisecond)
		s.now = func() time.Time { return at }
		s.t0 = base
		gs, err := s.Read()
		if err != nil || len(gs) != 1 {
			t.Fatal(err, gs)
		}
		g := gs[0]
		if g.MemUsed*100/g.MemTotal >= 93 || g.Temp >= 87 {
			t.Fatalf("at %dms the alert line would fire: %+v", ms, g)
		}
		if g.MemUsed <= 0 {
			t.Fatalf("no memory in use: %+v", g)
		}
	}
}

type clock struct {
	mu   sync.Mutex
	t    time.Time
	step chan time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// tick moves the clock to start+d and waits until Run has caught up.
func (c *clock) tick(d time.Duration, start time.Time) {
	c.mu.Lock()
	c.t = start.Add(d)
	c.mu.Unlock()
	c.step <- start.Add(d)
	c.step <- c.t // unbuffered: the second send returns once the first is handled
}

func runSim(t *testing.T) (*Sim, *proxy.Store, *clock, time.Time) {
	t.Helper()
	s := New()
	store := proxy.NewStore(256)
	start := time.Now()
	c := &clock{t: start, step: make(chan time.Time)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, store, c.now, c.step); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	c.tick(0, start)
	return s, store, c, start
}

func TestRunScript(t *testing.T) {
	_, store, c, start := runSim(t)
	flights := func() []string {
		var ms []string
		for _, f := range store.Flights() {
			ms = append(ms, f.Model)
		}
		return ms
	}

	c.tick(0, start)
	if len(flights()) != 0 {
		t.Fatalf("nothing should be in flight at t=0: %v", flights())
	}

	c.tick(sec(1.2), start)
	fl := store.Flights()
	if len(fl) != 1 || fl[0].Model != "llama3.1:8b" || !fl[0].Stream || !fl[0].FirstTok.IsZero() || fl[0].Tokens != 0 {
		t.Fatalf("t=1.2 want one 8b flight still in prefill: %+v", fl)
	}

	c.tick(sec(2), start)
	fl = store.Flights()
	if len(fl) != 1 || fl[0].FirstTok.IsZero() || fl[0].Tokens < 30 || fl[0].Tokens > 45 {
		t.Fatalf("t=2 want ~37 tokens (0.6s at 62/s): %+v", fl)
	}

	c.tick(sec(5.2), start)
	rs := store.Recent(5)
	if len(rs) != 1 || rs[0].Model != "llama3.1:8b" || rs[0].OutTk != 220 || rs[0].TokSec < 61 || rs[0].TokSec > 63 {
		t.Fatalf("t=5.2 8b should have finished at 62 tok/s: %+v", rs)
	}
	if rs[0].Prompt == "" || rs[0].Completion == "" {
		t.Error("inspector text missing")
	}
	// the 32b is mid-prefill (ttft 2.1s from t=3), plus the long prompt from 4.6
	if got := flights(); len(got) != 2 {
		t.Fatalf("t=5.2 want two 32b flights, got %v", got)
	}

	c.tick(sec(8.5), start)
	var long proxy.Request
	for _, r := range store.Recent(10) {
		if r.PromptTk == 3980 {
			long = r
		}
	}
	if long.Model != "qwen3:32b" || long.CtxPercent() != 97 {
		t.Fatalf("t=8.5 want a 3980-token 32b request at ctx 97%%: %+v", long)
	}
}

func TestRunQueueWindow(t *testing.T) {
	s, store, c, start := runSim(t)
	c.tick(sec(31), start)
	n := 0
	for _, f := range store.Flights() {
		if f.Model == llamaModel {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("t=31 want four gpt-oss flights, got %d", n)
	}
	rows, _, _ := s.Scan()
	for _, r := range rows {
		if r.Name == llamaModel && (r.Busy != 2 || r.Queued != 2 || r.Slots != 2) {
			t.Fatalf("scan during the queue: %+v", r)
		}
	}
}

func TestRunLoops(t *testing.T) {
	_, store, c, start := runSim(t)
	for ms := 0; ms <= 85000; ms += 50 {
		c.tick(time.Duration(ms)*time.Millisecond, start)
		for _, f := range store.Flights() {
			if f.Started.Before(start) {
				t.Fatalf("flight from before the run: %+v", f)
			}
		}
	}
	// two loops done, third 5s in
	done := store.Recent(100)
	want := 2*len(script) + 1 // plus the 8b request, done at 4.95s
	if len(done) != want {
		t.Fatalf("want %d finished requests after 85s, got %d", want, len(done))
	}
	for _, r := range done {
		if r.When.After(c.now()) {
			t.Fatalf("request from the future: %+v", r)
		}
	}
}

func TestRunNoLeakedFlights(t *testing.T) {
	_, store, c, start := runSim(t)
	// a stalled UI: one huge jump over two whole loops
	c.tick(0, start)
	c.tick(2*Loop+sec(0.1), start)
	if fl := store.Flights(); len(fl) != 0 {
		t.Fatalf("flights left after a jump over whole loops: %+v", fl)
	}
	if n := len(store.Recent(100)); n != 2*len(script) {
		t.Fatalf("want %d requests recorded, got %d", 2*len(script), n)
	}

	c.tick(2*Loop+sec(30), start)
	if len(store.Flights()) == 0 {
		t.Fatal("expected flights mid-loop")
	}
}

func TestRunCancelEndsFlights(t *testing.T) {
	s := New()
	store := proxy.NewStore(8)
	start := time.Now()
	c := &clock{t: start, step: make(chan time.Time)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, store, c.now, c.step); close(done) }()
	c.tick(0, start)
	c.tick(sec(2), start)
	if len(store.Flights()) == 0 {
		t.Fatal("setup: want a flight")
	}
	cancel()
	<-done
	if fl := store.Flights(); len(fl) != 0 {
		t.Fatalf("flights after cancel: %+v", fl)
	}
}

func TestUnloadAndReadd(t *testing.T) {
	s, _, c, start := runSim(t)
	c.tick(sec(2), start)
	rows, alive, err := s.Scan()
	if err != nil || len(alive) != 1 || alive[0] != "demo" || len(rows) != 4 {
		t.Fatalf("scan: %v %v %d", err, alive, len(rows))
	}
	for _, r := range rows {
		if r.From != "demo" {
			t.Errorf("row %s from %q", r.Name, r.From)
		}
	}
	var drop func() error
	for _, r := range rows {
		if r.Name == "qwen3:32b" {
			drop = r.Unload
		}
	}
	if drop == nil || drop() != nil {
		t.Fatal("32b has no working unload")
	}
	c.tick(sec(10), start)
	rows, _, _ = s.Scan()
	if len(rows) != 3 || byRow(rows, "qwen3:32b") {
		t.Fatalf("32b should be gone for the rest of the loop: %d rows", len(rows))
	}
	g1, _ := s.Read()
	c.tick(Loop+sec(1), start)
	rows, _, _ = s.Scan()
	if len(rows) != 4 || !byRow(rows, "qwen3:32b") {
		t.Fatalf("32b should be back next loop: %d rows", len(rows))
	}
	g2, _ := s.Read()
	if g2[0].MemUsed-g1[0].MemUsed < 7000 {
		t.Errorf("gpu memory should drop with the unloaded model: %d vs %d", g1[0].MemUsed, g2[0].MemUsed)
	}
}

func byRow(rows []sources.Row, name string) bool {
	for _, r := range rows {
		if r.Name == name {
			return true
		}
	}
	return false
}

func TestNothingBesidesTheStore(t *testing.T) {
	s := New()
	if !s.Available() || s.OnDisk() == 0 {
		t.Fatal("sim should report a gpu and models on disk")
	}
	rows, _, _ := s.Scan()
	for _, r := range rows {
		if strings.Contains(r.Base, ":") {
			t.Errorf("row base %q looks like a real host", r.Base)
		}
	}
}
