package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/demo"
	"github.com/eladser/mtop/internal/proxy"
)

var demoBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// demoAt plays the sim into a fresh demo app up to offset d, polling every
// 1.5s and sampling live rows every 250ms like the real loop does.
func demoAt(t *testing.T, d time.Duration, w, h, sel int) *App {
	t.Helper()
	cur := demoBase
	now := func() time.Time { return cur }
	sim := demo.New()
	store := proxy.NewStore(256)
	store.SetNow(now)
	a := New(sim, sim, store, "", "test", 0, nil, 93, 87, true)
	a.SetDemo()
	a.SetTarget("demo")
	a.now, a.start, a.w, a.h = now, demoBase, w, h

	step := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sim.Run(ctx, store, now, step); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	for ms := time.Duration(0); ms <= d; ms += 50 * time.Millisecond {
		cur = demoBase.Add(ms)
		step <- cur
		step <- cur // returns once the first is handled
		if ms%(250*time.Millisecond) == 0 {
			a.Update(liveTick{})
		}
		if ms%pollEvery == 0 {
			a.Update(a.poll())
		}
	}
	for i, r := range a.rows {
		if !r.Expires.IsZero() {
			a.rows[i].Expires = time.Now().Add(r.Expires.Sub(cur)) // ttl text counts from the wall clock
		}
	}
	a.sel = sel
	a.energyWh = 0 // wall-clock driven, would make the frame vary
	return a
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("view mismatch, run with -update to see the diff\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func widest(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		n = max(n, len([]rune(l)))
	}
	return n
}

func TestGoldenDemo(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	// 24s: 8b decoding on a throttled card, 32b selected with its cpu verdict
	a := demoAt(t, 24*time.Second, 100, 30, 2)
	got := stripANSI(a.View().Content)
	checkGolden(t, "view_demo.golden", got)
	for _, want := range []string{"MODELS  7 on disk  DEMO", "GPU  DEMO", "Simulated GPU (24 GB)", "hw thermal slowdown",
		"↳ 38% of model on CPU", "▶", "DEMO: simulated data, no server or GPU needed · q quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("demo view lacks %q", want)
		}
	}
}

func TestDemoQueueVerdict(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	for _, tt := range []struct {
		at   time.Duration
		want bool
	}{{30 * time.Second, false}, {33 * time.Second, true}, {37 * time.Second, true}} {
		got := stripANSI(demoAt(t, tt.at, 100, 30, 3).View().Content)
		if has := strings.Contains(got, "↳ 2 requests queued"); has != tt.want {
			t.Errorf("t=%v queue verdict shown=%v want %v\n%s", tt.at, has, tt.want, got)
		}
	}
}

func TestDemoThrottleWindow(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	for _, tt := range []struct {
		at   time.Duration
		want bool
	}{{18 * time.Second, false}, {22 * time.Second, true}, {26 * time.Second, true}, {33 * time.Second, false}} {
		got := stripANSI(demoAt(t, tt.at, 100, 30, 0).View().Content)
		if has := strings.Contains(got, "hw thermal slowdown"); has != tt.want {
			t.Errorf("t=%v throttle line shown=%v want %v", tt.at, has, tt.want)
		}
	}
}

func TestDemo80Cols(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	// live rows with the tail dropped, and the cpu verdict under the table
	a := demoAt(t, 24*time.Second, 80, 24, 2)
	got := stripANSI(a.View().Content)
	checkGolden(t, "view_live_80.golden", got)
	if w := widest(got); w > 80 {
		t.Errorf("a line is %d wide at 80 cols", w)
	}
	// every verdict string fits the 29 chars the models pane gives it
	a = demoAt(t, 33*time.Second, 80, 24, 3)
	got = stripANSI(a.View().Content)
	checkGolden(t, "view_verdicts_80.golden", got)
	if w := widest(got); w > 80 {
		t.Errorf("a line is %d wide at 80 cols", w)
	}
	if !strings.Contains(got, "↳ 2 requests queued") {
		t.Errorf("no queue verdict at 33s:\n%s", got)
	}
}
