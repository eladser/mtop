package sources

import (
	"io"
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
		if r.URL.Path == "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[{"id":"qwen2.5-7b","state":"loaded","quantization":"Q4_K_M"},{"id":"phi-4","state":"not-loaded"}]}`))
	}))
	defer lms.Close()

	s := New([]*ollama.Client{ollama.New(oll.URL)}, lcpp.URL, lms.URL, "", "", "", "")
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

	rows, alive, err := New([]*ollama.Client{ollama.New(a.URL), ollama.New(b.URL)}, "", "", "", "", "", "").Scan()
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

func TestScanLlamacppRouterMode(t *testing.T) {
	lcpp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			w.Write([]byte(`{"model_path":"","default_generation_settings":{"n_ctx":0}}`))
		case "/models":
			w.Write([]byte(`{"data":[
				{"id":"a","path":"/models/a.gguf","status":{"value":"loaded"}},
				{"id":"b","path":"/models/b.gguf","status":{"value":"unloaded"}}
			]}`))
		}
	}))
	defer lcpp.Close()

	s := New(nil, lcpp.URL, "", "", "", "", "")
	rows, ok := s.scanLlamacpp()
	if !ok {
		t.Fatal("expected router mode to report alive")
	}
	if len(rows) != 1 || rows[0].Name != "a.gguf" {
		t.Fatalf("expected only the loaded model, got %+v", rows)
	}
}

func TestScanLlamacppRouterIdle(t *testing.T) {
	lcpp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			w.Write([]byte(`{"model_path":""}`))
		case "/models":
			w.Write([]byte(`{"data":[{"id":"b","path":"/models/b.gguf","status":{"value":"unloaded"}}]}`))
		}
	}))
	defer lcpp.Close()

	rows, ok := New(nil, lcpp.URL, "", "", "", "", "").scanLlamacpp()
	if !ok || len(rows) != 0 {
		t.Fatalf("idle router: want alive with no rows, got ok=%v rows=%+v", ok, rows)
	}
}

func TestScanLMStudioV1(t *testing.T) {
	lms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"models":[
			{"key":"qwen2.5-7b","quantization":{"name":"Q4_K_M"},"loaded_instances":[{"id":"qwen2.5-7b"}]},
			{"key":"phi-4","quantization":{"name":"Q4_K_M"},"loaded_instances":[]}
		]}`))
	}))
	defer lms.Close()

	s := New(nil, "", lms.URL, "", "", "", "")
	rows, ok := s.scanLMStudio()
	if !ok {
		t.Fatal("expected lm studio to report alive")
	}
	if len(rows) != 1 || rows[0].Name != "qwen2.5-7b" || rows[0].Quant != "Q4_K_M" {
		t.Fatalf("bad rows: %+v", rows)
	}
}

func TestScanLMStudioFallsBackToV0(t *testing.T) {
	lms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[{"id":"phi-4","state":"loaded","quantization":"Q4_K_M"}]}`))
	}))
	defer lms.Close()

	s := New(nil, "", lms.URL, "", "", "", "")
	rows, ok := s.scanLMStudio()
	if !ok {
		t.Fatal("expected lm studio to report alive")
	}
	if len(rows) != 1 || rows[0].Name != "phi-4" {
		t.Fatalf("bad rows: %+v", rows)
	}
}

func TestScanVllmMetricFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `vllm:gpu_cache_usage_perc{model_name="m"} 0.5`+"\n"+`vllm:num_requests_running{model_name="m"} 1`+"\n")
	}))
	defer srv.Close()

	s := New(nil, "", "", srv.URL, "", "", "")
	rows, ok := s.scanVllm()
	if !ok || len(rows) != 1 || !strings.Contains(rows[0].Note, "cache 50%") {
		t.Fatalf("expected old metric name to be used as fallback: %+v ok=%v", rows, ok)
	}
}

func TestScanVllmNewMetricName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `vllm:kv_cache_usage_perc{model_name="m"} 0.75`+"\n"+`vllm:num_requests_running{model_name="m"} 2`+"\n")
	}))
	defer srv.Close()

	s := New(nil, "", "", srv.URL, "", "", "")
	rows, ok := s.scanVllm()
	if !ok || len(rows) != 1 || !strings.Contains(rows[0].Note, "cache 75%") {
		t.Fatalf("expected new metric name to be read: %+v ok=%v", rows, ok)
	}
}

func TestScanVllmRequiresVllmKey(t *testing.T) {
	// old Lemonade builds answer /metrics on :8000 too, without any vllm: keys
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `lemonade_requests_total{model="m"} 3`+"\n")
	}))
	defer srv.Close()

	s := New(nil, "", "", srv.URL, "", "", "")
	if rows, ok := s.scanVllm(); ok || len(rows) != 0 {
		t.Fatalf("expected no vllm row without a vllm: key, got %+v ok=%v", rows, ok)
	}
}

func TestScanLlamaswap(t *testing.T) {
	var unloaded string
	swap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/running":
			w.Write([]byte(`{"running":[{"model":"qwen3:32b","state":"ready","ttl":300}]}`))
		case r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/models/unload/"):
			unloaded = strings.TrimPrefix(r.URL.Path, "/api/models/unload/")
			w.Write([]byte("OK"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer swap.Close()

	s := New(nil, "", "", "", swap.URL, "", "")
	rows, ok := s.scanLlamaswap()
	if !ok || len(rows) != 1 || rows[0].Name != "qwen3:32b" || !strings.Contains(rows[0].Note, "ttl 300s") {
		t.Fatalf("bad llama-swap row: %+v ok=%v", rows, ok)
	}
	if err := rows[0].Unload(); err != nil || unloaded != "qwen3:32b" {
		t.Fatalf("unload didn't hit the per-model endpoint: err=%v unloaded=%q", err, unloaded)
	}
}

func TestScanLlamaswapFallsBackToLlamacppURL(t *testing.T) {
	swap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/running" {
			w.Write([]byte(`{"running":[{"model":"m","state":"ready","ttl":0}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer swap.Close()

	// no -llamaswap set, so it rides on -llamacpp since that's llama-swap's default port too
	s := New(nil, swap.URL, "", "", "", "", "")
	rows, ok := s.scanLlamaswap()
	if !ok || len(rows) != 1 || rows[0].Name != "m" {
		t.Fatalf("expected fallback to llamacpp url to work: %+v ok=%v", rows, ok)
	}
}

func TestScanLemonade(t *testing.T) {
	lem := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"all_models_loaded":[{"model_name":"Qwen3-0.6B-GGUF","device":"gpu","recipe_options":{"ctx_size":8192}}]}`))
	}))
	defer lem.Close()

	s := New(nil, "", "", "", "", lem.URL, "")
	rows, ok := s.scanLemonade()
	if !ok || len(rows) != 1 || rows[0].Name != "Qwen3-0.6B-GGUF" || !strings.Contains(rows[0].Note, "gpu") || !strings.Contains(rows[0].Note, "8192") {
		t.Fatalf("bad lemonade row: %+v ok=%v", rows, ok)
	}
}

func TestScanSglang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Write([]byte(`{"model_path":"/models/Meta-Llama-3-8B"}`))
		case "/metrics":
			io.WriteString(w, "sglang:token_usage 0.4\nsglang:num_running_reqs 2\n")
		}
	}))
	defer srv.Close()

	s := New(nil, "", "", "", "", "", srv.URL)
	rows, ok := s.scanSglang()
	if !ok || len(rows) != 1 || rows[0].Name != "Meta-Llama-3-8B" || !strings.Contains(rows[0].Note, "kv 40%") || !strings.Contains(rows[0].Note, "2 running") {
		t.Fatalf("bad sglang row: %+v ok=%v", rows, ok)
	}
}

func TestScanSglangUnderscorePrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Write([]byte(`{"model_path":"/models/m"}`))
		case "/metrics":
			io.WriteString(w, "sglang_token_usage 0.1\nsglang_num_running_reqs 1\n")
		}
	}))
	defer srv.Close()

	s := New(nil, "", "", "", "", "", srv.URL)
	rows, ok := s.scanSglang()
	if !ok || len(rows) != 1 || !strings.Contains(rows[0].Note, "kv 10%") {
		t.Fatalf("expected underscore-prefixed metrics to be read: %+v ok=%v", rows, ok)
	}
}

func TestGetJSONNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := New(nil, "", "", "", "", "", "")
	var v any
	if err := s.getJSON(srv.URL, &v); err == nil {
		t.Fatal("expected an error on non-2xx status")
	}
}

func TestGetPromLabeledNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	s := New(nil, "", "", "", "", "", "")
	if _, err := s.getPromLabeled(srv.URL); err == nil {
		t.Fatal("expected an error on non-2xx status")
	}
}

func TestScanDeadSources(t *testing.T) {
	s := New([]*ollama.Client{ollama.New("http://127.0.0.1:1")}, "http://127.0.0.1:1", "", "", "", "", "")
	rows, alive, ollErr := s.Scan()
	if ollErr == nil {
		t.Fatal("expected ollama error")
	}
	if len(rows) != 0 || len(alive) != 0 {
		t.Fatalf("nothing should be found: %v %v", rows, alive)
	}
}
