package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/eladser/mtop/internal/proxy"
	"github.com/eladser/mtop/internal/sources"
)

func TestModelLine(t *testing.T) {
	a := &App{}
	tests := []struct {
		name string
		row  sources.Row
		want []string // substrings the line must contain
	}{
		{
			name: "full gpu",
			row:  sources.Row{Name: "qwen3:0.6b", From: "ollama", VRAM: 1 << 30, Quant: "Q4_K_M"},
			want: []string{"1.0G"},
		},
		{
			name: "partial offload shows cpu percent and ttl",
			row:  sources.Row{Name: "qwen3:32b", From: "ollama", VRAM: 8 << 30, CPU: 38, Expires: time.Now().Add(4*time.Minute + 12*time.Second)},
			want: []string{"8.0G", "cpu 38%"},
		},
		{
			name: "fully on cpu shows cpu in vram cell",
			row:  sources.Row{Name: "qwen3:70b", From: "ollama", VRAM: 0, CPU: 100},
			want: []string{"cpu"},
		},
		{
			name: "non-ollama source with no vram info shows a dash",
			row:  sources.Row{Name: "phi-4", From: "lm studio", VRAM: 0},
			want: []string{"—"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := a.modelLine(tt.row, false)
			for _, w := range tt.want {
				if !strings.Contains(line, w) {
					t.Fatalf("line %q missing %q", line, w)
				}
			}
		})
	}
}

func TestNoteLine(t *testing.T) {
	long := "1 running · 3 queued · draft 71%"
	a := &App{w: 100, h: 40, rows: []sources.Row{{Name: "Qwen3-8B-Q4_K_M.gguf", From: "llama.cpp", Note: long}}}
	pane := func() []string { return strings.Split(stripANSI(a.modelsPane((a.w-6)/2)), "\n") }

	lines := pane()
	if len(lines) != 4 || strings.TrimSpace(lines[3]) != long {
		t.Fatalf("want the full note on its own line under the row, got:\n%s", strings.Join(lines, "\n"))
	}
	cut := a.reqRows()

	a.rows[0].Note = "1 running"
	if lines := pane(); len(lines) != 3 {
		t.Fatalf("a note that fits shouldn't add a line, got:\n%s", strings.Join(lines, "\n"))
	}
	if got := a.reqRows(); got != cut+1 {
		t.Fatalf("reqRows is %d with the note line and %d without, want one less", cut, got)
	}

	a.rows = nil
	if lines := pane(); len(lines) != 2 {
		t.Fatalf("no rows shouldn't add a line, got:\n%s", strings.Join(lines, "\n"))
	}
}

func TestModelsTitleFits(t *testing.T) {
	a := &App{w: 100, h: 30, alive: []string{"ollama", "llama.cpp", "lm studio"}, disk: 12}
	title := func() string { return strings.Split(stripANSI(a.modelsPane((a.w-6)/2)), "\n")[0] }
	lineW := func() int { return (a.w-6)/2 - 4 }

	if got := title(); len([]rune(got)) > lineW() || !strings.Contains(got, "ollama · llama.cpp · lm studio") {
		t.Fatalf("title %q is wider than %d or lost the servers", got, lineW())
	}

	a.w = 80
	if got := title(); len([]rune(got)) > lineW() || !strings.HasPrefix(got, "MODELS  ollama") || !strings.HasSuffix(got, "…") {
		t.Fatalf("title %q should cut the server list to %d", got, lineW())
	}

	a.w = 160
	if got, want := title(), "MODELS  ollama · llama.cpp · lm studio  12 on disk"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestControlBytesStripped(t *testing.T) {
	name, note := "n\x1b[2J", "evil\x1b]52;c;AAAA\x07 end"
	a := &App{w: 100, h: 40, rows: []sources.Row{{Name: name, From: "llama-swap", Note: note}}}
	out := stripANSI(a.modelsPane((a.w - 6) / 2))
	lines := strings.Split(out, "\n")
	if strings.ContainsAny(out, "\x1b\x07") || len(lines) != 4 || strings.TrimSpace(lines[3]) != "evil]52;c;AAAA end" {
		t.Fatalf("models pane leaks control bytes or lost the note line:\n%q", out)
	}
	if a.rows[0].Name != name || a.rows[0].Note != note {
		t.Fatal("the row should keep the server's raw strings")
	}

	store := proxy.NewStore(5)
	store.Add(proxy.Request{When: time.Now(), Model: name})
	a = &App{store: store, inspect: true, w: 100, h: 40}
	for _, out := range []string{a.requestsPane(), a.byModelPane(), a.inspectorPane()} {
		if out = stripANSI(out); strings.ContainsAny(out, "\x1b\x07") || !strings.Contains(out, "n[2J") {
			t.Fatalf("model not cleaned in:\n%q", out)
		}
	}
}

func TestClean(t *testing.T) {
	tests := []struct{ in, want string }{
		{"qwen-中文 ünï", "qwen-中文 ünï"},
		{"a\tb\nc\rd\x00e\x7ff", "abcdef"},
		{"x\u009b2Jy\u0085z", "x2Jyz"},
		{"bad\x9butf8", "bad�utf8"},
	}
	for _, tt := range tests {
		if got := clean(tt.in); got != tt.want {
			t.Fatalf("clean(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPartialCPU(t *testing.T) {
	tests := []struct {
		cpu  int
		want bool
	}{
		{0, false},
		{1, true},
		{38, true},
		{99, true},
		{100, false},
	}
	for _, tt := range tests {
		if got := partialCPU(sources.Row{CPU: tt.cpu}); got != tt.want {
			t.Fatalf("cpu=%d: got %v want %v", tt.cpu, got, tt.want)
		}
	}
}

func TestCtxMarker(t *testing.T) {
	tests := []struct {
		name string
		req  proxy.Request
		want string
	}{
		{"no ctx known", proxy.Request{PromptTk: 100}, ""},
		{"low usage", proxy.Request{PromptTk: 100, Ctx: 4096}, ""},
		{"heuristic high usage", proxy.Request{PromptTk: 3980, Ctx: 4096}, "ctx 97%"},
		{"confirmed rejection", proxy.Request{Overflow: true, PromptTk: 5000, Ctx: 4096}, "ctx over, rejected"},
		{"rejection wins even with a low ctx percent", proxy.Request{Overflow: true, PromptTk: 10, Ctx: 4096}, "ctx over, rejected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ctxMarker(tt.req); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestInspectorShowsThinking(t *testing.T) {
	store := proxy.NewStore(5)
	store.Add(proxy.Request{When: time.Now(), Model: "m", Prompt: "q", Thinking: "pondering", Completion: "answer"})
	a := &App{store: store, inspect: true, w: 80}
	out := a.inspectorPane()
	for _, w := range []string{"thinking", "pondering", "completion", "answer"} {
		if !strings.Contains(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	store.Add(proxy.Request{When: time.Now(), Model: "m", Prompt: "q", Completion: "answer"})
	if strings.Contains(a.inspectorPane(), "thinking") {
		t.Fatal("thinking block should be hidden when empty")
	}
}
