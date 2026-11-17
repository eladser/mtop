package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
	"github.com/eladser/mtop/internal/verdict"
)

func TestMatchRow(t *testing.T) {
	rows := []sources.Row{
		{Name: "qwen3:0.6b", From: "ollama", Base: "127.0.0.1:11434"},
		{Name: "llama3:latest", From: "ollama", Base: "127.0.0.1:11434"},
		{Name: "phi-4", From: "lm studio", Base: "127.0.0.1:1234"},
		{Name: "gguf-path-name", Alias: "my-alias", Aliases: []string{"gguf-path-name", "short"}, From: "llama.cpp", Base: "127.0.0.1:8080"},
		{Name: "other", From: "llama.cpp", Base: "127.0.0.1:8080"},
		{Name: "solo", From: "vllm", Base: "127.0.0.1:8000"},
	}
	tests := []struct {
		name, target, model string
		want                int
	}{
		{"exact name", "127.0.0.1:11434", "qwen3:0.6b", 0},
		{"case insensitive", "127.0.0.1:11434", "QWEN3:0.6B", 0},
		{"latest suffix", "127.0.0.1:11434", "llama3", 1},
		{"alias", "127.0.0.1:8080", "my-alias", 3},
		{"aliases list", "127.0.0.1:8080", "short", 3},
		{"single candidate, unknown name", "127.0.0.1:8000", "whatever-the-client-sent", -1},
		{"two candidates, unknown name", "127.0.0.1:11434", "mystery", -1},
		{"two llama.cpp candidates, unknown name", "127.0.0.1:8080", "mystery", -1},
		{"model on another server", "127.0.0.1:11434", "phi-4", -1},
		{"target mismatch", "127.0.0.1:9999", "qwen3:0.6b", -1},
		{"no target", "", "qwen3:0.6b", -1},
		{"empty model, single candidate", "127.0.0.1:8000", "", 5},
		{"empty model, two candidates", "127.0.0.1:11434", "", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{rows: rows}
			a.SetTarget(tt.target)
			if got := a.matchRow(tt.model); got != tt.want {
				t.Fatalf("matchRow(%q) = %d, want %d", tt.model, got, tt.want)
			}
		})
	}
}

func TestMatchRowTargetURL(t *testing.T) {
	a := &App{rows: []sources.Row{{Name: "m", Base: "localhost:11434"}}}
	a.SetTarget("http://localhost:11434")
	if a.matchRow("m") != 0 {
		t.Fatal("url target should reduce to host:port")
	}
}

func TestVerdictLineWidth(t *testing.T) {
	texts := []string{
		"rejected: context full", "prompt cut to fit context", "100% of model on CPU",
		"999 requests queued", "999 requests, 999 slots", "context 99% full",
	}
	a := &App{w: 80, h: 30, rows: []sources.Row{{Name: "m", From: "ollama"}}}
	lineW := (a.w-6)/2 - 4
	if lineW != 33 {
		t.Fatalf("lineW %d", lineW)
	}
	for _, s := range texts {
		a.verdicts = map[string]verdict.Verdict{"ollama|m": {Text: s}}
		l := a.verdictLine(lineW)
		if n := len([]rune(l)); n > lineW {
			t.Errorf("%q is %d wide, pane is %d", l, n, lineW)
		}
		if got := strings.TrimPrefix(l, "  ↳ "); got != s {
			t.Errorf("%q was cut to %q", s, got)
		}
	}
	a.verdicts = nil
	if a.verdictLine(lineW) != "" {
		t.Error("line without a verdict")
	}
}

func TestVerdictStyle(t *testing.T) {
	if verdictStyle(verdict.Red).GetForeground() != warn {
		t.Error("red should use the warn color")
	}
	if verdictStyle(verdict.Amber).GetForeground() != amberSt.GetForeground() || amberSt.GetForeground() == warn {
		t.Error("amber should be its own color")
	}
}

func TestModelsPaneVerdicts(t *testing.T) {
	a := &App{w: 100, h: 30, sel: 1, rows: []sources.Row{
		{Name: "a", From: "ollama", Size: "1b"},
		{Name: "b", From: "ollama", Size: "1b"},
		{Name: "c", From: "ollama", Size: "1b"},
	}}
	a.verdicts = map[string]verdict.Verdict{
		"ollama|a": {Text: "38% of model on CPU", Level: verdict.Red},
		"ollama|b": {Text: "3 requests queued", Level: verdict.Amber},
	}
	lines := strings.Split(stripANSI(a.modelsPane(47)), "\n")
	if !strings.HasPrefix(lines[2], "! a") || !strings.HasPrefix(lines[3], "▸ b") || !strings.HasPrefix(lines[4], "  c") {
		t.Fatalf("prefixes:\n%s", strings.Join(lines, "\n"))
	}
	if last := lines[len(lines)-1]; last != "  ↳ 3 requests queued" {
		t.Fatalf("selected row text missing, got %q", last)
	}
	if strings.Contains(strings.Join(lines, "\n"), "38% of model") {
		t.Fatal("unselected row's text shown")
	}
}

func TestReqRowsLines(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	mk := func(nrows, ngpu int) *App {
		a := &App{w: 100, h: 40, store: proxy.NewStore(8), gpu: gpu.New()}
		for i := 0; i < nrows; i++ {
			a.rows = append(a.rows, sources.Row{Name: fmt.Sprintf("q%d", i), From: "llama.cpp"})
		}
		for i := 0; i < ngpu; i++ {
			a.gpus = append(a.gpus, gpu.Stats{Name: fmt.Sprintf("g%d", i)})
		}
		return a
	}
	// models side is the tall one: 6 rows + 2
	a := mk(6, 1)
	n0 := a.reqRows()
	a.rows[0].Note = "1 running · 3 queued · draft 71%"
	n1 := a.reqRows()
	a.verdicts = map[string]verdict.Verdict{"llama.cpp|q0": {Text: "3 requests queued"}}
	n2 := a.reqRows()
	if n1 != n0-1 || n2 != n1-1 {
		t.Errorf("note and verdict lines should cost a request row each: %d %d %d", n0, n1, n2)
	}
	// gpu side is the tall one: 4 gpus
	b := mk(1, 4)
	m0 := b.reqRows()
	b.throttled = []uint8{gpu.ThrottleThermal, 0, 0, 0}
	if m1 := b.reqRows(); m1 != m0-1 {
		t.Errorf("throttle line should cost a request row: %d vs %d", m1, m0)
	}
}

func TestThrottleText(t *testing.T) {
	tests := []struct {
		bits uint8
		want string
	}{
		{0, ""},
		{gpu.ThrottleThermal, "hw thermal slowdown"},
		{gpu.ThrottlePower, "hw power brake"},
		{gpu.ThrottleHW, "hw slowdown"},
		{gpu.ThrottleHW | gpu.ThrottlePower, "hw power brake"},
		{gpu.ThrottleHW | gpu.ThrottleThermal, "hw thermal slowdown"},
	}
	for _, tt := range tests {
		a := &App{throttled: []uint8{tt.bits}}
		got := a.throttleText(0)
		if got != tt.want || len(got) > 29 {
			t.Errorf("bits %d: %q, want %q", tt.bits, got, tt.want)
		}
	}
	if (&App{}).throttleText(3) != "" {
		t.Error("out of range")
	}
}

func TestThrottleLinesGated(t *testing.T) {
	t.Setenv("MTOP_GPU", "apple")
	a := &App{gpu: gpu.New(), gpus: []gpu.Stats{{}}, throttled: []uint8{gpu.ThrottleHW}}
	if a.throttleLines() != 1 {
		t.Fatal("want 1")
	}
	a.gpuErr = fmt.Errorf("boom")
	if a.throttleLines() != 0 {
		t.Fatal("the pane draws no line on a read error, so it can't count one")
	}
	if (&App{throttled: []uint8{1}}).throttleLines() != 0 {
		t.Fatal("nil gpu")
	}
}

func TestJudge(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	store := proxy.NewStore(16)
	store.Preload([]proxy.Request{
		{When: now.Add(-time.Minute), Model: "big", PromptTk: 5000, Ctx: 4096, Overflow: true},
		{When: now.Add(-time.Minute), Model: "unrelated", PromptTk: 5000, Ctx: 4096, Overflow: true},
	})
	a := &App{store: store, now: func() time.Time { return now }, start: now.Add(-time.Hour),
		rows: []sources.Row{
			{Name: "big", From: "ollama", Base: "h:1", VRAM: 1},
			{Name: "split", From: "ollama", Base: "h:1", VRAM: 1, CPU: 40},
		},
		gpus: []gpu.Stats{{Util: 90, Throttle: gpu.ThrottleThermal}}}
	a.SetTarget("http://h:1")
	a.judge()
	if v := a.verdicts["ollama|big"]; v.Kind != verdict.CtxFull {
		t.Errorf("big: %+v", v)
	}
	if v := a.verdicts["ollama|split"]; v.Kind != verdict.CPU {
		t.Errorf("split: %+v", v)
	}
	if len(a.verdicts) != 2 {
		t.Errorf("a request for an unmatched model leaked into a verdict: %+v", a.verdicts)
	}
	if a.throttleText(0) != "" {
		t.Error("throttle showed on the first poll")
	}
	a.judge()
	a.judge()
	if a.throttleText(0) != "hw thermal slowdown" {
		t.Error("throttle should show on the third poll")
	}
}

func TestMatchRowSwappedModel(t *testing.T) {
	// A overflowed, ollama swapped to B: only B is loaded, A's request must not land on it
	a := &App{rows: []sources.Row{{Name: "b:latest", From: "ollama", Base: "127.0.0.1:11434"}}}
	a.SetTarget("127.0.0.1:11434")
	if got := a.matchRow("a:latest"); got != -1 {
		t.Fatalf("matched swapped-out model to row %d", got)
	}
}

func TestJudgeUsesRowCtxForAlias(t *testing.T) {
	store := proxy.NewStore(10)
	store.Add(proxy.Request{Model: "llama3.1", When: time.Now(), PromptTk: 4096, Overflow: true})
	a := &App{store: store, rows: []sources.Row{{Name: "llama3.1:latest", From: "ollama", Base: "127.0.0.1:11434", Ctx: 4096}}}
	// session started before the request, or the rules skip it as history
	a.start = time.Now().Add(-time.Minute)
	a.SetTarget("127.0.0.1:11434")
	a.judge()
	if _, ok := a.verdicts["ollama|llama3.1:latest"]; !ok {
		t.Fatalf("no verdict for an alias client: %+v", a.verdicts)
	}
}
