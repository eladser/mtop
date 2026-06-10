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
	t.Helper()
	p, err := New(upstream, store)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(p.Handler())
	t.Cleanup(front.Close)
	return front
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
