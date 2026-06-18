package sources

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eladser/mtop/internal/ollama"
)

func TestParseProm(t *testing.T) {
	body := `# HELP llamacpp:requests_processing whatever
llamacpp:requests_processing 2
llamacpp:kv_cache_usage_ratio 0.34
vllm:num_requests_running{model_name="meta-llama/Llama-3-8B",engine="0"} 1
vllm:num_requests_running{model_name="meta-llama/Llama-3-8B",engine="1"} 2
garbage line without value x
`
	p := parseProm(body)
	if p.vals["llamacpp:requests_processing"] != 2 {
		t.Fatalf("bad value: %v", p.vals)
	}
	if p.vals["vllm:num_requests_running"] != 3 {
		t.Fatalf("labeled values should sum: %v", p.vals)
	}
	if p.label("model_name") != "meta-llama/Llama-3-8B" {
		t.Fatalf("bad label: %q", p.label("model_name"))
	}
}

func TestScanMergesSources(t *testing.T) {
	oll := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"qwen3:0.6b","size_vram":1000,"details":{"parameter_size":"0.6B","quantization_level":"Q4_K_M"}}]}`))
		default:
			w.Write([]byte(`{"models":[]}`))
		}
	}))
	defer oll.Close()

	lcpp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			w.Write([]byte(`{"model_path":"/models/llama-3-8b.Q4_K_M.gguf","default_generation_settings":{"n_ctx":8192}}`))
		case "/metrics":
			w.Write([]byte("llamacpp:kv_cache_usage_ratio 0.5\nllamacpp:requests_processing 1\n"))
		}
	}))
	defer lcpp.Close()

	lms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[{"id":"qwen2.5-7b","state":"loaded","quantization":"Q4_K_M"},{"id":"phi-4","state":"not-loaded"}]}`))
	}))
	defer lms.Close()

	s := New([]*ollama.Client{ollama.New(oll.URL)}, lcpp.URL, lms.URL, "")
	rows, alive, ollErr := s.Scan()
	if ollErr != nil {
		t.Fatal(ollErr)
	}
	if len(alive) != 3 {
		t.Fatalf("expected 3 sources alive, got %v", alive)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (lm studio skips unloaded), got %d: %+v", len(rows), rows)
	}
	if rows[0].From != "ollama" || rows[0].Unload == nil {
		t.Fatalf("ollama row should be unloadable: %+v", rows[0])
	}
	if rows[1].Name != "llama-3-8b.Q4_K_M.gguf" || rows[1].Note == "" {
		t.Fatalf("bad llama.cpp row: %+v", rows[1])
	}
	if rows[2].From != "lm studio" || rows[2].Unload != nil {
		t.Fatalf("lm studio row should not be unloadable: %+v", rows[2])
	}
}

func TestScanMultiHost(t *testing.T) {
	mk := func(model string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/ps" {
				w.Write([]byte(`{"models":[{"name":"` + model + `","details":{}}]}`))
			} else {
				w.Write([]byte(`{"models":[]}`))
			}
		}))
	}
	a, b := mk("alpha"), mk("beta")
	defer a.Close()
	defer b.Close()

	rows, alive, err := New([]*ollama.Client{ollama.New(a.URL), ollama.New(b.URL)}, "", "", "").Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(alive) != 1 {
		t.Fatalf("want 2 rows, 1 alive entry; got %d rows %v", len(rows), alive)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.From, "ollama@") {
			t.Fatalf("multi-host rows should be labelled by host, got %q", r.From)
		}
	}
}

func TestScanDeadSources(t *testing.T) {
	s := New([]*ollama.Client{ollama.New("http://127.0.0.1:1")}, "http://127.0.0.1:1", "", "")
	rows, alive, ollErr := s.Scan()
	if ollErr == nil {
		t.Fatal("expected ollama error")
	}
	if len(rows) != 0 || len(alive) != 0 {
		t.Fatalf("nothing should be found: %v %v", rows, alive)
	}
}
