package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/proxy"
)

var liveNow = time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC)

func TestLiveRate(t *testing.T) {
	s := []liveSample{
		{liveNow.Add(-5 * time.Second), 0},
		{liveNow.Add(-1500 * time.Millisecond), 10},
		{liveNow, 40},
	}
	if r, ok := liveRate(s, liveNow); !ok || r != 20 {
		t.Fatalf("rate %v %v, want 20 (the 5s-old sample is outside the window)", r, ok)
	}
	if _, ok := liveRate(s[:1], liveNow); ok {
		t.Fatal("one sample is no rate")
	}
	if _, ok := liveRate([]liveSample{{liveNow, 1}, {liveNow, 5}}, liveNow); ok {
		t.Fatal("zero span is no rate")
	}
}

func TestFlightRateWaits(t *testing.T) {
	a := &App{store: proxy.NewStore(4), live: map[uint64][]liveSample{
		1: {{liveNow.Add(-time.Second), 0}, {liveNow, 9}},
	}}
	f := proxy.Flight{ID: 1, Stream: true, FirstTok: liveNow.Add(-time.Second), Tokens: 9}
	if r, ok := a.flightRate(f, liveNow); !ok || r != 9 {
		t.Fatalf("rate %v %v", r, ok)
	}
	f.Tokens = 2
	if _, ok := a.flightRate(f, liveNow); ok {
		t.Fatal("under 3 chunks should wait")
	}
	f.Tokens = 9
	f.FirstTok = liveNow.Add(-200 * time.Millisecond)
	if _, ok := a.flightRate(f, liveNow); ok {
		t.Fatal("under 300ms should wait")
	}
}

func TestLiveSplit(t *testing.T) {
	tests := []struct{ f, r, live, more, fin int }{
		{1, 10, 1, 0, 9},
		{8, 10, 8, 0, 2},
		{9, 10, 7, 2, 2},
		{30, 10, 7, 23, 2},
		{2, 4, 2, 0, 2},
		{3, 4, 1, 2, 2},
	}
	for _, tt := range tests {
		l, m, f := liveSplit(tt.f, tt.r)
		if l != tt.live || m != tt.more || f != tt.fin {
			t.Errorf("F=%d R=%d: got %d/%d/%d, want %d/%d/%d", tt.f, tt.r, l, m, f, tt.live, tt.more, tt.fin)
		}
		if shown := l + f; m > 0 && shown+1 != tt.r || m == 0 && shown != tt.r {
			t.Errorf("F=%d R=%d: rows %d don't fill the pane", tt.f, tt.r, shown)
		}
	}
}

func TestLiveTailFits(t *testing.T) {
	start := liveNow.Add(-3900 * time.Millisecond)
	wait := proxy.Flight{Stream: true, Started: start}
	dec := proxy.Flight{Stream: true, Started: start, FirstTok: start.Add(420 * time.Millisecond), Tokens: 5}
	plain := proxy.Flight{Started: start}
	tests := []struct {
		name string
		f    proxy.Flight
		w    int
		want string
	}{
		{"wait 100", wait, 22, "prefill"},
		{"wait 80", wait, 2, ""},
		{"plain 100", plain, 22, "not streaming"},
		{"plain 90", plain, 12, ""},
		{"decoding 100", dec, 22, "ttft 0.42s · dec 3.5s"},
		{"decoding 90", dec, 12, "ttft 0.42s"},
		{"decoding 80", dec, 2, ""},
	}
	for _, tt := range tests {
		if got := liveTail(tt.f, liveNow, tt.w); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
	// the budget the pane computes: 100, 90, 80 columns
	for cols, want := range map[int]int{100: 22, 90: 12, 80: 2} {
		if got := cols - 6 - 70 - 2; got != want {
			t.Errorf("%d cols: budget %d, want %d", cols, got, want)
		}
	}
}

func liveApp(w int) *App {
	a := fixedApp()
	a.w = w
	a.now = func() time.Time { return liveNow }
	a.store.SetNow(a.now)
	return a
}

func TestLiveRowsCleanModel(t *testing.T) {
	a := liveApp(100)
	a.store.Begin("/api/chat", "evil\x1b]0;pwn\x07name", true, liveNow.Add(-time.Second))
	rows, _ := a.liveRows(10)
	if len(rows) != 1 || strings.ContainsAny(stripANSI(rows[0]), "\x1b\x07") {
		t.Fatalf("live row leaks control bytes: %q", rows)
	}
	a.store.Begin("/api/chat", strings.Repeat("m", 60), true, liveNow)
	rows, _ = a.liveRows(10)
	if !strings.Contains(stripANSI(rows[1]), strings.Repeat("m", 25)+"…") {
		t.Fatalf("model not truncated: %q", rows[1])
	}
}

func TestLiveRowsOverflowRow(t *testing.T) {
	a := liveApp(100)
	for i := 0; i < 9; i++ {
		a.store.Begin("/api/chat", fmt.Sprintf("m%d", i), true, liveNow)
	}
	rows, fin := a.liveRows(10)
	if len(rows) != 8 || fin != 2 || !strings.Contains(stripANSI(rows[7]), "+2 more live") {
		t.Fatalf("rows %d fin %d last %q", len(rows), fin, rows[len(rows)-1])
	}
	if got := len(rows) + fin; got != 10 {
		t.Fatalf("pane rows %d, want 10", got)
	}
}

func TestLiveRowWidth(t *testing.T) {
	for _, w := range []int{80, 90, 100} {
		a := liveApp(w)
		f := a.store.Begin("/api/chat", "llama3.1:8b", true, liveNow.Add(-3900*time.Millisecond))
		f.Tick(5, liveNow.Add(-3480*time.Millisecond))
		for _, row := range a.mustLiveRows() {
			if n := len([]rune(stripANSI(row))); n > w-6 {
				t.Errorf("%d cols: row is %d wide: %q", w, n, row)
			}
		}
	}
}

func (a *App) mustLiveRows() []string {
	rows, _ := a.liveRows(10)
	return rows
}

func TestGoldenLive(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	a := liveApp(100)
	a.store.Begin("/api/chat", "llama3.1:8b", true, liveNow.Add(-2300*time.Millisecond))
	d := a.store.Begin("/v1/chat/completions", "qwen3:32b", true, liveNow.Add(-3900*time.Millisecond))
	d.Tick(1, liveNow.Add(-3480*time.Millisecond))
	d.Tick(39, liveNow)
	a.store.Begin("/v1/chat/completions", "gpt-oss-20b", false, liveNow.Add(-65*time.Second))
	a.live = map[uint64][]liveSample{
		2: {{liveNow.Add(-1500 * time.Millisecond), 10}, {liveNow, 40}},
	}
	got := stripANSI(a.View().Content)
	const golden = "testdata/view_live.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("view mismatch, run with -update to see the diff\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSampleLivePrunes(t *testing.T) {
	a := liveApp(100)
	f := a.store.Begin("/api/chat", "m", true, liveNow)
	for i := 0; i < 20; i++ {
		f.Tick(1, liveNow)
		now := liveNow.Add(time.Duration(i) * liveEvery)
		a.now = func() time.Time { return now }
		a.sampleLive()
	}
	if n := len(a.live[1]); n > 10 {
		t.Fatalf("%d samples kept, want about 2s worth", n)
	}
	f.End()
	a.sampleLive()
	if len(a.live) != 0 {
		t.Fatal("finished flight should drop its samples")
	}
}

func BenchmarkViewLive(b *testing.B) {
	b.Setenv("MTOP_GPU", "apple")
	a := liveApp(100)
	for i := 0; i < 3; i++ {
		f := a.store.Begin("/api/chat", fmt.Sprintf("model-%d", i), true, liveNow.Add(-time.Second))
		f.Tick(10, liveNow.Add(-500*time.Millisecond))

	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		a.View()
	}
}

func TestLiveClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		3200 * time.Millisecond: "3.2s",
		41 * time.Second:        "41.0s",
		125 * time.Second:       "2m05s",
		-time.Second:            "0.0s",
	} {
		if got := liveClock(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
