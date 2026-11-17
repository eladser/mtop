package verdict

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func one(t *testing.T, tr *Tracker, now time.Time, m Model) (Verdict, bool) {
	t.Helper()
	m.Key = "ollama|m"
	v, ok := tr.Update(now, []Model{m})["ollama|m"]
	return v, ok
}

func TestRules(t *testing.T) {
	ago := func(d time.Duration) time.Time { return t0.Add(-d) }
	tests := []struct {
		name  string
		m     Model
		start time.Time // zero means long before t0
		want  string    // "" = no verdict
		kind  Kind
	}{
		{name: "overflow", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 4096, PromptTk: 5000, Overflow: true}}}, want: "rejected: context full", kind: CtxFull},
		{name: "overflow ctx unknown", m: Model{Reqs: []Req{{When: ago(time.Minute), Overflow: true, PromptTk: 5000}}}},
		{name: "overflow too old", m: Model{Reqs: []Req{{When: ago(5*time.Minute + time.Second), Ctx: 4096, Overflow: true}}}},
		{name: "overflow before session", start: t0.Add(-time.Minute), m: Model{Reqs: []Req{{When: ago(2 * time.Minute), Ctx: 4096, Overflow: true}}}},
		{name: "cut at ctx", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 4096, PromptTk: 4096}}}, want: "prompt cut to fit context", kind: CtxCut},
		{name: "cut over ctx", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 4096, PromptTk: 5000}}}, want: "prompt cut to fit context", kind: CtxCut},
		{name: "cut ctx unknown", m: Model{Reqs: []Req{{When: ago(time.Minute), PromptTk: 5000}}}},
		{name: "full beats cut", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 200}, {When: ago(time.Minute), Ctx: 100, Overflow: true}}}, want: "rejected: context full", kind: CtxFull},
		{name: "cpu 9", m: Model{CPU: 9, VRAM: 1}},
		{name: "cpu 10", m: Model{CPU: 10, VRAM: 1}, want: "10% of model on CPU", kind: CPU},
		{name: "cpu 38", m: Model{CPU: 38, VRAM: 1}, want: "38% of model on CPU", kind: CPU},
		{name: "cpu 99", m: Model{CPU: 99, VRAM: 1}, want: "99% of model on CPU", kind: CPU},
		{name: "cpu 100", m: Model{CPU: 100, VRAM: 1}},
		{name: "cpu no vram", m: Model{CPU: 50}},
		{name: "ctx 79", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 79}}}},
		{name: "ctx 80", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 80}}}, want: "context 80% full", kind: CtxNear},
		{name: "ctx 94", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 94}}}, want: "context 94% full", kind: CtxNear},
		{name: "ctx 99", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 99}}}, want: "context 99% full", kind: CtxNear},
		{name: "ctx 100 is a cut", m: Model{Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 100}}}, want: "prompt cut to fit context", kind: CtxCut},
		{name: "ctx old", m: Model{Reqs: []Req{{When: ago(10*time.Minute + time.Second), Ctx: 100, PromptTk: 90}}}},
		{name: "ctx 9 min old", m: Model{Reqs: []Req{{When: ago(9 * time.Minute), Ctx: 100, PromptTk: 90}}}, want: "context 90% full", kind: CtxNear},
		{name: "newer lower request", m: Model{Reqs: []Req{{When: ago(5 * time.Minute), Ctx: 100, PromptTk: 90}, {When: ago(time.Minute), Ctx: 100, PromptTk: 20}}}},
		{name: "ctx unknown", m: Model{Reqs: []Req{{When: ago(time.Minute), PromptTk: 90}}}},
		{name: "cpu beats near", m: Model{CPU: 38, VRAM: 1, Reqs: []Req{{When: ago(time.Minute), Ctx: 100, PromptTk: 90}}}, want: "38% of model on CPU", kind: CPU},
		{name: "failure beats cpu", m: Model{CPU: 38, VRAM: 1, Reqs: []Req{{When: ago(time.Minute), Ctx: 100, Overflow: true}}}, want: "rejected: context full", kind: CtxFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := tt.start
			if start.IsZero() {
				start = t0.Add(-time.Hour)
			}
			v, ok := one(t, New(start), t0, tt.m)
			if tt.want == "" {
				if ok {
					t.Fatalf("got %q, want none", v.Text)
				}
				return
			}
			if !ok || v.Text != tt.want || v.Kind != tt.kind {
				t.Fatalf("got %+v ok=%v, want %q", v, ok, tt.want)
			}
		})
	}
}

func TestLevels(t *testing.T) {
	start := t0.Add(-time.Hour)
	if v, _ := one(t, New(start), t0, Model{CPU: 38, VRAM: 1}); v.Level != Red {
		t.Errorf("cpu level %v", v.Level)
	}
	r := Model{Reqs: []Req{{When: t0, Ctx: 100, PromptTk: 90}}}
	if v, _ := one(t, New(start), t0, r); v.Level != Amber {
		t.Errorf("near level %v", v.Level)
	}
}

func TestQueueDebounce(t *testing.T) {
	tr := New(t0)
	now := t0
	step := func(m Model) (Verdict, bool) {
		now = now.Add(1500 * time.Millisecond)
		return one(t, tr, now, m)
	}
	q := Model{Queued: 3}
	if _, ok := step(q); ok {
		t.Fatal("single poll fired")
	}
	if v, ok := step(q); !ok || v.Text != "3 requests queued" || v.Level != Amber {
		t.Fatalf("2nd poll: %+v %v", v, ok)
	}
	if v, ok := step(Model{}); !ok || v.Text != "3 requests queued" {
		t.Fatalf("first clean poll should hold: %+v %v", v, ok)
	}
	if _, ok := step(Model{}); ok {
		t.Fatal("two clean polls should clear")
	}
	// blip: queued, clean, queued never reaches 2 in a row
	step(q)
	step(Model{})
	if _, ok := step(q); ok {
		t.Fatal("blip fired")
	}
}

func TestQueueWording(t *testing.T) {
	tr := New(t0)
	one(t, tr, t0, Model{Queued: 1})
	if v, _ := one(t, tr, t0, Model{Queued: 1}); v.Text != "1 request queued" {
		t.Fatalf("got %q", v.Text)
	}
}

func TestSlotsRule(t *testing.T) {
	tr := New(t0)
	m := Model{Slots: 4, Flights: 6}
	one(t, tr, t0, m)
	if v, ok := one(t, tr, t0, m); !ok || v.Text != "6 requests, 4 slots" {
		t.Fatalf("got %+v %v", v, ok)
	}
	// flights equal to slots is full, not over
	tr = New(t0)
	m = Model{Slots: 4, Flights: 4}
	one(t, tr, t0, m)
	if _, ok := one(t, tr, t0, m); ok {
		t.Fatal("fired at flights == slots")
	}
	// proxy-only counts without a slot count say nothing
	tr = New(t0)
	m = Model{Flights: 9}
	one(t, tr, t0, m)
	if _, ok := one(t, tr, t0, m); ok {
		t.Fatal("fired with Slots == 0")
	}
}

func TestPriority(t *testing.T) {
	tr := New(t0.Add(-time.Hour))
	m := Model{CPU: 38, VRAM: 1, Queued: 2, Reqs: []Req{{When: t0, Ctx: 100, PromptTk: 90}}}
	one(t, tr, t0, m)
	if v, _ := one(t, tr, t0, m); v.Kind != CPU {
		t.Fatalf("cpu should win, got %+v", v)
	}
	m.CPU = 0
	if v, _ := one(t, tr, t0, m); v.Kind != Queue {
		t.Fatalf("queue should beat ctx near, got %+v", v)
	}
	m.Queued = 0
	one(t, tr, t0, m)
	if v, _ := one(t, tr, t0, m); v.Kind != CtxNear {
		t.Fatalf("got %+v", v)
	}
}

func TestCtxNearHysteresis(t *testing.T) {
	tr := New(t0.Add(-time.Hour))
	now := t0.Add(time.Minute)
	r := func(at time.Time, tk int) Req { return Req{When: at, Ctx: 100, PromptTk: tk} }
	reqs := []Req{r(t0, 90)}
	if _, ok := one(t, tr, now, Model{Reqs: reqs}); !ok {
		t.Fatal("90% should fire")
	}
	reqs = append(reqs, r(t0.Add(10*time.Second), 75))
	if _, ok := one(t, tr, now, Model{Reqs: reqs}); !ok {
		t.Fatal("75% after a shown verdict should hold")
	}
	reqs = append(reqs, r(t0.Add(20*time.Second), 69))
	if _, ok := one(t, tr, now, Model{Reqs: reqs}); ok {
		t.Fatal("69% should clear")
	}
	// 75% alone never fires
	tr = New(t0.Add(-time.Hour))
	if _, ok := one(t, tr, now, Model{Reqs: []Req{r(t0, 75)}}); ok {
		t.Fatal("75% fired cold")
	}
}

func TestCtxClearsByTime(t *testing.T) {
	tr := New(t0.Add(-time.Hour))
	m := Model{Reqs: []Req{{When: t0, Ctx: 100, Overflow: true}}}
	if _, ok := one(t, tr, t0.Add(4*time.Minute), m); !ok {
		t.Fatal("should show at 4m")
	}
	if _, ok := one(t, tr, t0.Add(5*time.Minute), m); ok {
		t.Fatal("should clear at 5m")
	}
}

func TestMultiHostKeys(t *testing.T) {
	tr := New(t0)
	ms := []Model{
		{Key: "ollama@a:11434|qwen3:0.6b", Queued: 2},
		{Key: "ollama@b:11434|qwen3:0.6b"},
	}
	tr.Update(t0, ms)
	got := tr.Update(t0.Add(time.Second), ms)
	if _, ok := got["ollama@a:11434|qwen3:0.6b"]; !ok {
		t.Fatal("host a should have a verdict")
	}
	if _, ok := got["ollama@b:11434|qwen3:0.6b"]; ok {
		t.Fatal("host b picked up host a's state")
	}
}

func TestUnloadDropsState(t *testing.T) {
	tr := New(t0.Add(-time.Hour))
	req := Req{When: t0, Ctx: 100, Overflow: true}
	m := Model{Key: "k", Queued: 1, Reqs: []Req{req}}
	tr.Update(t0, []Model{m})
	if _, ok := tr.Update(t0.Add(time.Second), []Model{m})["k"]; !ok {
		t.Fatal("setup: expected a verdict")
	}
	tr.Update(t0.Add(2*time.Second), nil)
	// reloaded: the old overflow is stale and the queue debounce starts over
	m.Queued = 0
	if _, ok := tr.Update(t0.Add(3*time.Second), []Model{m})["k"]; ok {
		t.Fatal("state survived an unload")
	}
	m.Reqs = []Req{{When: t0.Add(4 * time.Second), Ctx: 100, Overflow: true}}
	if _, ok := tr.Update(t0.Add(5*time.Second), []Model{m})["k"]; !ok {
		t.Fatal("request after the reload ignored")
	}
}
