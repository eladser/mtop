package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func proxyFor(t *testing.T, upstream string, store *Store) *httptest.Server {
	return proxyForInspect(t, upstream, store, false)
}

func proxyForInspect(t *testing.T, upstream string, store *Store, inspect bool) *httptest.Server {
	t.Helper()
	p, err := New(upstream, store, inspect)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)
	return front
}

func TestClipStripsControlBytes(t *testing.T) {
	got := clip("hi\x1b]52;c;evil\x07 there\nok\tgo")
	if strings.ContainsRune(got, 0x1b) || strings.ContainsRune(got, 0x07) {
		t.Fatalf("escape bytes survived: %q", got)
	}
	if !strings.Contains(got, "hi") || !strings.Contains(got, "there") || !strings.Contains(got, "\n") || !strings.Contains(got, "\t") {
		t.Fatalf("dropped text it should keep: %q", got)
	}
}

func TestInspectCaptures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"m","done":false,"response":"hello "}`+"\n")
		io.WriteString(w, `{"model":"m","done":false,"response":"world"}`+"\n")
		io.WriteString(w, `{"model":"m","done":true,"eval_count":2,"eval_duration":1000000000,"load_duration":500000000,"prompt_eval_duration":300000000}`+"\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyForInspect(t, upstream.URL, store, true)
	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{"prompt":"say hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := store.Recent(1)[0]
	if r.Prompt != "say hi" || r.Completion != "hello world" {
		t.Fatalf("bad capture: prompt=%q completion=%q", r.Prompt, r.Completion)
	}
	if r.Load == 0 || r.PromptEval == 0 {
		t.Fatalf("timings not captured: %+v", r)
	}
}

func TestInspectCapturesThinkingOllamaChat(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"qwen3:4b","done":false,"message":{"role":"assistant","thinking":"let me "}}`+"\n")
		io.WriteString(w, `{"model":"qwen3:4b","done":false,"message":{"role":"assistant","thinking":"think"}}`+"\n")
		io.WriteString(w, `{"model":"qwen3:4b","done":false,"message":{"role":"assistant","content":"42"}}`+"\n")
		io.WriteString(w, `{"model":"qwen3:4b","done":true,"eval_count":10,"eval_duration":1000000000}`+"\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyForInspect(t, upstream.URL, store, true)
	resp, err := http.Post(front.URL+"/api/chat", "application/json", strings.NewReader(`{"messages":[{"role":"user","content":"what's 6*7"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := store.Recent(1)[0]
	if r.Thinking != "let me think" || r.Completion != "42" {
		t.Fatalf("bad capture: thinking=%q completion=%q", r.Thinking, r.Completion)
	}
}

func TestStreamingChunks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		io.WriteString(w, `{"model":"llama3:8b","done":false,"response":"hi"}`+"\n")
		io.WriteString(w, `{"model":"llama3:8b","done":true,"prompt_eval_count":12,"eval_count":100,"eval_duration":2000000000,"total_duration":2500000000}`+"\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `"response":"hi"`) {
		t.Fatalf("client should see the stream untouched, got: %s", body)
	}

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "llama3:8b" || r.OutTk != 100 || r.PromptTk != 12 {
		t.Fatalf("bad record: %+v", r)
	}
	// 100 tokens over 2s
	if r.TokSec < 49.9 || r.TokSec > 50.1 {
		t.Fatalf("bad tok/s: %f", r.TokSec)
	}
}

func TestNonStreamingNoTrailingNewline(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"qwen3:4b","done":true,"prompt_eval_count":5,"eval_count":40,"eval_duration":1000000000,"total_duration":1200000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/chat", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 || reqs[0].OutTk != 40 || reqs[0].Path != "/api/chat" {
		t.Fatalf("bad record: %+v", reqs)
	}
}

func TestOpenAIStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"qwen2.5-7b\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		// tok/s for openai-style requests is wall-clock; answering within
		// one clock tick (~0.5ms on windows) makes it zero, so take a
		// moment like a real model would
		time.Sleep(20 * time.Millisecond)
		io.WriteString(w, "data: {\"model\":\"qwen2.5-7b\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":42}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "qwen2.5-7b" || r.OutTk != 42 || r.PromptTk != 9 {
		t.Fatalf("bad record: %+v", r)
	}
	if r.TokSec <= 0 {
		t.Fatalf("wall-clock tok/s should be positive: %+v", r)
	}
}

func TestOpenAIStreamingNoUsageUsesTimings(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"llama-3-8b\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"llama-3-8b\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"timings\":{\"prompt_n\":9,\"predicted_n\":42,\"predicted_per_second\":55.5}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "llama-3-8b" || r.OutTk != 42 || r.PromptTk != 9 || r.TokSec != 55.5 || r.Estimated {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestOpenAIStreamingNoUsageNoTimingsEstimates(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 5; i++ {
			io.WriteString(w, "data: {\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	if r := reqs[0]; r.OutTk != 5 || !r.Estimated {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestResponsesStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"type":"response.output_text.delta","delta":"hi"}`+"\n\n")
		io.WriteString(w, `data: {"type":"response.completed","response":{"model":"gpt-4.1","usage":{"input_tokens":11,"output_tokens":22}}}`+"\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "gpt-4.1" || r.OutTk != 22 || r.PromptTk != 11 {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestResponsesNonStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"resp_1","model":"gpt-4.1","usage":{"input_tokens":3,"output_tokens":7}}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	reqs := store.Recent(1)
	if len(reqs) != 1 || reqs[0].OutTk != 7 || reqs[0].PromptTk != 3 {
		t.Fatalf("bad record: %+v", reqs)
	}
}

func TestByModelAndProm(t *testing.T) {
	store := NewStore(10)
	store.Add(Request{Model: "a", TokSec: 10, OutTk: 100})
	store.Add(Request{Model: "a", TokSec: 20, OutTk: 100})
	store.Add(Request{Model: "b", TokSec: 5, OutTk: 50})

	stats := store.ByModel()
	if len(stats) != 2 || stats[0].Model != "a" {
		t.Fatalf("busiest first: %+v", stats)
	}
	if stats[0].Count != 2 || stats[0].AvgTok != 15 || stats[0].OutTk != 200 {
		t.Fatalf("bad aggregate: %+v", stats[0])
	}

	text := store.PromText()
	for _, want := range []string{`mtop_requests_total{model="a"} 2`, `mtop_tokens_out_total{model="b"} 50`, `quantile="0.95"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("metrics should not reach the upstream")
	}))
	defer upstream.Close()

	store := NewStore(10)
	store.Add(Request{Model: "x", TokSec: 7, OutTk: 10})
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Get(front.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "mtop_requests_total") {
		t.Fatalf("not prometheus output: %s", body)
	}
}

func TestGuardBlocksCrossOriginAndRebinding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[]}`))
	}))
	defer upstream.Close()

	front := proxyFor(t, upstream.URL, NewStore(10))

	do := func(setup func(*http.Request)) int {
		req, _ := http.NewRequest("POST", front.URL+"/api/delete", strings.NewReader(`{"name":"x"}`))
		setup(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	// a normal cli-style call: loopback host, no Origin
	if code := do(func(r *http.Request) {}); code == http.StatusForbidden {
		t.Fatal("a plain loopback request should pass")
	}
	// a browser page on evil.com fetching the loopback proxy
	if code := do(func(r *http.Request) { r.Header.Set("Origin", "http://evil.com") }); code != http.StatusForbidden {
		t.Fatalf("cross-origin request should be blocked, got %d", code)
	}
	// dns rebinding: the rebound request still carries the attacker host
	if code := do(func(r *http.Request) { r.Host = "evil.com:4321" }); code != http.StatusForbidden {
		t.Fatalf("non-loopback host should be blocked, got %d", code)
	}
	// a local web app talking to it is fine
	if code := do(func(r *http.Request) { r.Header.Set("Origin", "http://localhost:3000") }); code == http.StatusForbidden {
		t.Fatal("a same-machine web app should pass")
	}
}

func TestHugeBodyStaysIntact(t *testing.T) {
	// past the buffer cap the tap stops looking for metrics, but the
	// client still has to receive every byte
	huge := strings.Repeat("x", 2<<20)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"big","response":"`+huge+`","done":true,"eval_count":5,"eval_duration":1000000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) < 2<<20 {
		t.Fatalf("client got %d bytes, expected the whole response", len(body))
	}
}

func TestLastSeen(t *testing.T) {
	store := NewStore(10)
	if !store.LastSeen("a").IsZero() {
		t.Fatal("unknown model should be zero")
	}
	store.Add(Request{Model: "a", When: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	store.Add(Request{Model: "b", When: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	store.Add(Request{Model: "a", When: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)})
	if got := store.LastSeen("a"); got.Day() != 3 {
		t.Fatalf("want newest entry, got %v", got)
	}
}

func TestOllamaHeuristicCtxOverflow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"qwen3:32b","done":true,"prompt_eval_count":3980,"eval_count":10,"eval_duration":1000000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	store.SetModels([]ModelInfo{{Name: "qwen3:32b", Ctx: 4096}})
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{"model":"qwen3:32b"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := store.Recent(1)[0]
	if r.Ctx != 4096 || r.PromptTk != 3980 {
		t.Fatalf("bad record: %+v", r)
	}
	if pct := r.CtxPercent(); pct != 97 {
		t.Fatalf("want 97%%, got %d%%", pct)
	}
	if !r.Overflowed() || r.Overflow {
		t.Fatalf("expected the heuristic (not a confirmed rejection) to fire: %+v", r)
	}
	if !strings.Contains(store.PromText(), `mtop_ctx_overflow_total{model="qwen3:32b"} 1`) {
		t.Fatalf("overflow counter missing:\n%s", store.PromText())
	}
}

func TestLlamaCppOverflowError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"type":"exceed_context_size_error","n_prompt_tokens":5000,"n_ctx":4096}}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "exceed_context_size_error") {
		t.Fatalf("client should still see the error body untouched, got: %s", body)
	}

	r := store.Recent(1)[0]
	if !r.Overflow || r.Ctx != 4096 || r.PromptTk != 5000 {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestVLLMOverflowError(t *testing.T) {
	body := `{"error":"This model's maximum context length is 4096 tokens."}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, body)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != body {
		t.Fatalf("bytes should pass through untouched: got %q want %q", got, body)
	}
	if r := store.Recent(1)[0]; !r.Overflow {
		t.Fatalf("expected overflow: %+v", r)
	}
}

func TestSGLangOverflowError(t *testing.T) {
	body := `{"error":"the request is longer than the model's context length"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, body)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != body {
		t.Fatalf("bytes should pass through untouched: got %q want %q", got, body)
	}
	if r := store.Recent(1)[0]; !r.Overflow {
		t.Fatalf("expected overflow: %+v", r)
	}
}

func TestOllamaTruncateFalseOverflowError(t *testing.T) {
	body := `{"error":"the input length exceeds the context length"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, body)
	}))
	defer upstream.Close()

	store := NewStore(10)
	store.SetModels([]ModelInfo{{Name: "m", Ctx: 4096}})
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(got) != body {
		t.Fatalf("bytes should pass through untouched: got %q want %q", got, body)
	}
	r := store.Recent(1)[0]
	if !r.Overflow || r.Model != "m" || r.Ctx != 4096 {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestNon400ErrorNotRecordedAsOverflow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if got := store.Recent(1); len(got) != 0 {
		t.Fatalf("an unrelated 404 shouldn't be recorded: %+v", got)
	}
}

func TestOpenAITimingsUsesCacheN(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"model\":\"llama-3-8b\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		io.WriteString(w, "data: {\"model\":\"llama-3-8b\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"timings\":{\"prompt_n\":9,\"cache_n\":40,\"predicted_n\":42,\"predicted_per_second\":55.5}}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	r := store.Recent(1)[0]
	if r.PromptTk != 49 {
		t.Fatalf("want prompt_n+cache_n=49, got %d", r.PromptTk)
	}
}

func TestMetricsIncludesCPUGauge(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("metrics should not reach the upstream")
	}))
	defer upstream.Close()

	store := NewStore(10)
	store.SetModels([]ModelInfo{{Name: "qwen3:32b", Ctx: 4096, CPU: 38}})
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Get(front.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), `mtop_model_cpu_percent{model="qwen3:32b"} 38`) {
		t.Fatalf("missing cpu gauge:\n%s", body)
	}
}

func TestPeekCapStillForwardsFullBody(t *testing.T) {
	old := maxPeek
	maxPeek = 16
	defer func() { maxPeek = old }()

	var got []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		io.WriteString(w, `{"model":"m","done":true,"eval_count":1,"eval_duration":1000000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	body := `{"model":"m","prompt":"` + strings.Repeat("x", 200) + `"}`
	resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if string(got) != body {
		t.Fatalf("upstream should get every byte past the peek cap: got %d bytes, want %d", len(got), len(body))
	}
}

func TestSmallBodyStillLabelsRejectedRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"type":"exceed_context_size_error","n_prompt_tokens":5000,"n_ctx":4096}}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if r := store.Recent(1)[0]; r.Model != "m" {
		t.Fatalf("expected the model label pulled from the request body: %+v", r)
	}
}

func TestOtherPathsPassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"models":[]}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)

	resp, err := http.Get(front.URL + "/api/tags")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(resp.Body)
	resp.Body.Close()

	if got := store.Recent(1); len(got) != 0 {
		t.Fatalf("tags should not be recorded: %+v", got)
	}
}

func post(t *testing.T, url, body string) string {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestInspectCapturesThinkingOllamaGenerate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"gpt-oss:20b","done":false,"thinking":"hm "}`+"\n")
		io.WriteString(w, `{"model":"gpt-oss:20b","done":false,"thinking":"ok"}`+"\n")
		io.WriteString(w, `{"model":"gpt-oss:20b","done":false,"response":"hi"}`+"\n")
		io.WriteString(w, `{"model":"gpt-oss:20b","done":true,"eval_count":3,"eval_duration":1000000000}`+"\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyForInspect(t, upstream.URL, store, true)
	body := post(t, front.URL+"/api/generate", `{"prompt":"hey"}`)
	if !strings.Contains(body, `"thinking":"hm "`) {
		t.Fatalf("stream should pass through untouched: %s", body)
	}
	r := store.Recent(1)[0]
	if r.Thinking != "hm ok" || r.Completion != "hi" || r.OutTk != 3 {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestOpenAIReasoningFieldVariants(t *testing.T) {
	tests := []struct {
		name  string
		field string // llama.cpp/sglang/old vllm vs ollama/new vllm
	}{
		{"reasoning_content", "reasoning_content"},
		{"reasoning", "reasoning"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := "data: {\"model\":\"r1\",\"choices\":[{\"delta\":{\"" + tt.field + "\":\"a\"}}]}\n\n" +
				"data: {\"model\":\"r1\",\"choices\":[{\"delta\":{\"" + tt.field + "\":\"b\"}}]}\n\n" +
				"data: {\"model\":\"r1\",\"choices\":[{\"delta\":{\"content\":\"42\"}}]}\n\n" +
				"data: [DONE]\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, stream)
			}))
			defer upstream.Close()

			store := NewStore(10)
			front := proxyForInspect(t, upstream.URL, store, true)
			if got := post(t, front.URL+"/v1/chat/completions", `{}`); got != stream {
				t.Fatalf("bytes changed: %q", got)
			}
			r := store.Recent(1)[0]
			if r.OutTk != 3 || !r.Estimated {
				t.Fatalf("reasoning chunks should count: %+v", r)
			}
			if r.Thinking != "ab" || r.Completion != "42" {
				t.Fatalf("bad capture: thinking=%q completion=%q", r.Thinking, r.Completion)
			}
		})
	}
}

func TestResponsesStreamingThinking(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"type":"response.reasoning_text.delta","delta":"think "}`+"\n\n")
		io.WriteString(w, `data: {"type":"response.reasoning_summary_text.delta","delta":"more"}`+"\n\n")
		io.WriteString(w, `data: {"type":"response.output_text.delta","delta":"hi"}`+"\n\n")
		io.WriteString(w, `data: {"type":"response.completed","response":{"model":"gpt-oss","usage":{"input_tokens":4,"output_tokens":9}}}`+"\n\n")
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyForInspect(t, upstream.URL, store, true)
	post(t, front.URL+"/v1/responses", `{"input":"yo"}`)
	r := store.Recent(1)[0]
	if r.Thinking != "think more" || r.Completion != "hi" || r.OutTk != 9 {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestResponsesNonStreamingThinking(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"gpt-oss","usage":{"input_tokens":3,"output_tokens":7},"output":[`+
			`{"type":"reasoning","summary":[{"type":"summary_text","text":"sum"}],"content":[{"type":"reasoning_text","text":"raw"}]},`+
			`{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyForInspect(t, upstream.URL, store, true)
	post(t, front.URL+"/v1/responses", `{}`)
	r := store.Recent(1)[0]
	if r.Thinking != "sumraw" || r.Completion != "answer" {
		t.Fatalf("bad record: %+v", r)
	}
}

func TestThinkingNotCapturedWithoutInspect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"m","done":true,"message":{"thinking":"x"},"eval_count":1,"eval_duration":1000000000}`)
	}))
	defer upstream.Close()

	store := NewStore(10)
	front := proxyFor(t, upstream.URL, store)
	post(t, front.URL+"/api/chat", `{}`)
	if r := store.Recent(1)[0]; r.Thinking != "" {
		t.Fatalf("thinking captured without -inspect: %q", r.Thinking)
	}
}

func TestPromptOfShapes(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"prompt", `{"prompt":"hi"}`, "hi"},
		{"string content", `{"messages":[{"role":"user","content":"a"},{"role":"assistant","content":"b"},{"role":"user","content":"c"}]}`, "c"},
		{"parts content", `{"messages":[{"role":"system","content":"s"},{"role":"user","content":[{"type":"text","text":"foo "},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"bar"}]}]}`, "foo bar"},
		{"responses string", `{"input":"hello"}`, "hello"},
		{"responses items string", `{"input":[{"role":"user","content":"one"},{"role":"assistant","content":"x"},{"role":"user","content":"two"}]}`, "two"},
		{"responses parts", `{"input":[{"role":"user","content":[{"type":"input_text","text":"pa"},{"type":"input_text","text":"rt"}]}]}`, "part"},
		{"none", `{"model":"m"}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := promptOf([]byte(tc.body)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOpenAIThinkingNotDoubled(t *testing.T) {
	for _, tc := range []struct{ name, delta, want string }{
		{"both equal", `"reasoning_content":"hmm","reasoning":"hmm"`, "hmm"},
		{"only reasoning", `"reasoning":"hmm"`, "hmm"},
		{"only reasoning_content", `"reasoning_content":"hmm"`, "hmm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tp := &tap{openai: true, inspect: true, store: NewStore(10), started: time.Now()}
			tp.record([]byte(`data: {"model":"m","choices":[{"delta":{` + tc.delta + `}}]}`))
			if got := tp.think.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
