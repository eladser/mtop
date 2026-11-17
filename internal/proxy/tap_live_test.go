package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func eventually(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for !cond() {
		if time.Now().After(end) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// gated upstream: every string sent on the channel is written and
// flushed; closing the channel ends the response.
func gated(t *testing.T, ctype string) (*httptest.Server, chan string) {
	t.Helper()
	return gatedH(t, ctype, true)
}

// early flushes the headers before the first chunk; without it the
// client sees nothing until the first write, like a server that
// answers only at the first token.
func gatedH(t *testing.T, ctype string, early bool) (*httptest.Server, chan string) {
	t.Helper()
	ch := make(chan string)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ctype)
		if early {
			w.(http.Flusher).Flush()
		}
		for {

			select {
			case s, ok := <-ch:
				if !ok {
					return
				}
				io.WriteString(w, s)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(up.Close)
	return up, ch
}

func oai(content string) string {
	return `data: {"model":"m","choices":[{"delta":{"content":"` + content + `"}}]}` + "\n\n"
}

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestLiveOllamaStream(t *testing.T) {
	up, ch := gated(t, "application/x-ndjson")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/api/chat", `{"model":"qwen","messages":[]}`)
	rd := bufio.NewReader(resp.Body)

	var got strings.Builder
	lines := []string{
		`{"model":"qwen","message":{"role":"assistant","content":"a"},"done":false}` + "\n",
		`{"model":"qwen","message":{"role":"assistant","content":"b"},"done":false}` + "\n",
		`{"model":"qwen","message":{"role":"assistant","content":"","thinking":"hm"},"done":false}` + "\n",
	}
	time.Sleep(5 * time.Millisecond)
	for k, l := range lines {
		ch <- l
		s, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		got.WriteString(s)
		fl := store.Flights()
		if len(fl) != 1 || fl[0].Tokens != k+1 || !fl[0].Stream {
			t.Fatalf("after chunk %d: %+v", k+1, fl)
		}
	}
	done := `{"model":"qwen","message":{"content":""},"done":true,"eval_count":3,"eval_duration":1000000000}` + "\n"
	ch <- done
	s, _ := rd.ReadString('\n')
	got.WriteString(s)
	close(ch)
	io.Copy(io.Discard, rd)

	if got.String() != strings.Join(lines, "")+done {
		t.Fatalf("client bytes changed: %q", got.String())
	}
	if len(store.Flights()) != 0 {
		t.Fatalf("flight should be gone once recorded: %+v", store.Flights())
	}
	r := store.Recent(1)
	if len(r) != 1 || r[0].TTFT <= 0 || r[0].OutTk != 3 {
		t.Fatalf("record: %+v", r)
	}
}

func TestLiveFlightWhileBlockedOnHeaders(t *testing.T) {
	up, ch := gatedH(t, "application/x-ndjson", false)
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	// the gated upstream sends no headers until its first write
	errc := make(chan error, 1)
	go func() {
		resp, err := http.Post(front.URL+"/api/generate", "application/json", strings.NewReader(`{"model":"slow"}`))
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		errc <- err
	}()
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 1 })
	f := store.Flights()[0]
	if f.Model != "slow" || f.Tokens != 0 || !f.FirstTok.IsZero() || !f.Stream {
		t.Fatalf("flight: %+v", f)
	}
	close(ch)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 })
}

func TestLiveStreamDefaults(t *testing.T) {
	tests := []struct {
		path, body string
		want       bool
	}{
		{"/api/generate", `{"model":"m"}`, true},
		{"/api/generate", `{"model":"m","stream":false}`, false},
		{"/v1/chat/completions", `{"model":"m"}`, false},
		{"/v1/chat/completions", `{"model":"m","stream":true}`, true},
		{"/v1/chat/completions", `not json`, false},
	}
	for _, tt := range tests {
		up, ch := gatedH(t, "application/json", false)
		store := NewStore(10)
		front := proxyFor(t, up.URL, store)
		go func() {
			resp, err := http.Post(front.URL+tt.path, "application/json", strings.NewReader(tt.body))
			if err == nil {
				resp.Body.Close()
			}
		}()
		eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 1 })
		if got := store.Flights()[0].Stream; got != tt.want {
			t.Errorf("%s %s: stream %v, want %v", tt.path, tt.body, got, tt.want)
		}
		close(ch)
	}
}

func TestLiveNonStreamedOllamaDoesNotTick(t *testing.T) {
	up, ch := gated(t, "application/json")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/api/generate", `{"model":"m","stream":false}`)
	ch <- `{"model":"m","response":"the whole answer","done":false}` + "\n"
	bufio.NewReader(resp.Body).ReadString('\n')
	f := store.Flights()
	if len(f) != 1 || f[0].Stream || f[0].Tokens != 0 {
		t.Fatalf("non-streamed flight ticked: %+v", f)
	}
	close(ch)
}

func TestLiveOpenAIChunks(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"m","stream":true}`)
	rd := bufio.NewReader(resp.Body)
	read := func() {
		rd.ReadString('\n')
		rd.ReadString('\n')
	}
	// role-only first chunk
	ch <- `data: {"model":"m","choices":[{"delta":{"role":"assistant","content":""}}]}` + "\n\n"
	read()
	if f := store.Flights()[0]; f.Tokens != 0 || !f.FirstTok.IsZero() {
		t.Fatalf("role-only chunk counted: %+v", f)
	}
	ch <- oai("a")
	read()
	ch <- `data: {"model":"m","choices":[{"delta":{"reasoning_content":"r"}}]}` + "\n\n"
	read()
	ch <- `data: {"model":"m","choices":[{"delta":{"reasoning":"r"}}]}` + "\n\n"
	read()
	if f := store.Flights()[0]; f.Tokens != 3 {
		t.Fatalf("tokens: %+v", f)
	}
	close(ch)
}

func TestLiveResponsesDeltasWithoutInspect(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/responses", `{"model":"m","stream":true}`)
	rd := bufio.NewReader(resp.Body)
	for k, typ := range []string{"response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta"} {
		ch <- "data: {\"type\":\"" + typ + "\",\"delta\":\"x\"}\n\n"
		rd.ReadString('\n')
		rd.ReadString('\n')
		if f := store.Flights()[0]; f.Tokens != k+1 {
			t.Fatalf("after %s: %+v", typ, f)
		}
	}
	// not a text event, and an empty delta
	ch <- "data: {\"type\":\"response.output_item.added\",\"delta\":{\"a\":1}}\n\n"
	rd.ReadString('\n')
	rd.ReadString('\n')
	ch <- "data: {\"type\":\"response.output_text.delta\",\"delta\":\"\"}\n\n"
	rd.ReadString('\n')
	rd.ReadString('\n')
	if f := store.Flights()[0]; f.Tokens != 3 {
		t.Fatalf("tokens: %+v", f)
	}
	close(ch)
}

func TestLiveIncludeUsageUsesTimings(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"m","stream":true}`)
	go func() {
		ch <- oai("a")
		ch <- `data: {"model":"m","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":42},"timings":{"predicted_n":42,"predicted_per_second":77.5}}` + "\n\n"
		ch <- "data: [DONE]\n\n"
		close(ch)
	}()
	io.Copy(io.Discard, resp.Body)
	r := store.Recent(1)
	if len(r) != 1 || r[0].TokSec != 77.5 || r[0].OutTk != 42 {
		t.Fatalf("record: %+v", r)
	}
}

func TestOpenAIDecodeOnlyRate(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"m","stream":true}`)
	go func() {
		time.Sleep(400 * time.Millisecond) // prefill
		ch <- oai("a")
		time.Sleep(100 * time.Millisecond)
		ch <- oai("b")
		ch <- `data: {"model":"m","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":100}}` + "\n\n"
		close(ch)
	}()
	io.Copy(io.Discard, resp.Body)
	r := store.Recent(1)
	if len(r) != 1 {
		t.Fatal("no record")
	}
	wall := float64(r[0].OutTk) / r[0].Total.Seconds()
	if r[0].TokSec < 1.5*wall || r[0].TTFT < 350*time.Millisecond {
		t.Fatalf("not decode-only: tok/s %.1f, wall %.1f, ttft %s", r[0].TokSec, wall, r[0].TTFT)
	}
}

func TestOpenAIWallRateWithoutFirstToken(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"m"}`)
	go func() {
		time.Sleep(60 * time.Millisecond)
		ch <- `data: {"model":"m","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":30}}` + "\n\n"
		close(ch)
	}()
	io.Copy(io.Discard, resp.Body)
	r := store.Recent(1)[0]
	if r.TTFT != 0 || r.TokSec != float64(r.OutTk)/r.Total.Seconds() {
		t.Fatalf("want wall time: %+v", r)
	}
}

func TestLiveModelFromFirstChunk(t *testing.T) {
	up, ch := gated(t, "text/event-stream")
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"req-name","stream":true}`)
	rd := bufio.NewReader(resp.Body)
	ch <- `data: {"model":"alias","choices":[{"delta":{"content":"a"}}]}` + "\n\n"
	rd.ReadString('\n')
	if f := store.Flights()[0]; f.Model != "alias" {
		t.Fatalf("model: %+v", f)
	}
	close(ch)

	up2, ch2 := gated(t, "text/event-stream")
	front2 := proxyFor(t, up2.URL, store)
	resp2 := postJSON(t, front2.URL+"/v1/chat/completions", `{{{`)
	rd2 := bufio.NewReader(resp2.Body)
	go func() { ch2 <- oai("a") }()
	rd2.ReadString('\n')
	eventually(t, 2*time.Second, func() bool {
		for _, f := range store.Flights() {
			if f.Model == "m" {
				return true
			}
		}
		return false
	})
	close(ch2)
}

func TestLiveCancels(t *testing.T) {
	for _, tt := range []struct {
		name, path, body, chunk string
		recorded                bool
	}{
		{"openai", "/v1/chat/completions", `{"model":"m","stream":true}`, oai("a"), true},
		{"ollama", "/api/chat", `{"model":"m"}`, `{"model":"m","message":{"content":"a"},"done":false}` + "\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			up, ch := gated(t, "text/event-stream")
			store := NewStore(10)
			front := proxyFor(t, up.URL, store)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "POST", front.URL+tt.path, strings.NewReader(tt.body))
			go func() { ch <- tt.chunk }()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			bufio.NewReader(resp.Body).ReadString('\n')
			if len(store.Flights()) != 1 {
				t.Fatal("no flight mid-stream")
			}
			cancel()
			resp.Body.Close()
			eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 })
			if got := len(store.Recent(5)) == 1; got != tt.recorded {
				t.Fatalf("recorded = %v, want %v", got, tt.recorded)
			}
		})
	}
}

func TestLiveClearedOnErrors(t *testing.T) {
	store := NewStore(10)
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	front := proxyFor(t, url, store)
	resp := postJSON(t, front.URL+"/api/chat", `{"model":"m"}`)
	io.Copy(io.Discard, resp.Body)
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 })

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad"}`, http.StatusBadRequest)
	}))
	defer up.Close()
	front2 := proxyFor(t, up.URL, store)
	resp = postJSON(t, front2.URL+"/v1/chat/completions", `{"model":"m"}`)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 })
	if len(store.Recent(5)) != 0 {
		t.Fatal("neither failure should record")
	}
}

func TestLiveOverflowRecordsAndClears(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"error":{"type":"exceed_context_size_error","n_prompt_tokens":5000,"n_ctx":4096}}`)
	}))
	defer up.Close()
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	resp := postJSON(t, front.URL+"/v1/chat/completions", `{"model":"m"}`)
	io.Copy(io.Discard, resp.Body)
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 && len(store.Recent(5)) == 1 })
}

func TestLiveNonStreamingSurvivesGuardAndRecords(t *testing.T) {
	up, ch := gatedH(t, "application/json", false)
	store := NewStore(10)
	front := proxyFor(t, up.URL, store)
	go func() {
		resp, err := http.Post(front.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"model":"m"}`))
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 1 })
	store.SetNow(func() time.Time { return time.Now().Add(time.Hour) })
	if len(store.Flights()) != 1 {
		t.Fatal("non-streaming flight hidden by the guard")
	}
	ch <- `{"model":"m","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":5}}`
	close(ch)
	eventually(t, 2*time.Second, func() bool { return len(store.Flights()) == 0 && len(store.Recent(5)) == 1 })
}

func benchTap(b *testing.B, line []byte, openai bool, live bool) {
	s := NewStore(10)
	tp := &tap{store: s, openai: openai, stream: true, started: time.Now()}
	if live {
		tp.live = s.Begin("/x", "m", true, time.Now())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tp.record(line)
	}
}

var (
	ollamaLine = []byte(`{"model":"qwen3","created_at":"2026-01-01T12:00:00Z","message":{"role":"assistant","content":" word"},"done":false}` + "\n")
	openaiLine = []byte(`data: {"id":"c1","object":"chat.completion.chunk","model":"qwen3","choices":[{"index":0,"delta":{"content":" word"}}]}` + "\n")
)

func BenchmarkTapOllamaChunk(b *testing.B) {
	b.Run("plain", func(b *testing.B) { benchTap(b, ollamaLine, false, false) })
	b.Run("live", func(b *testing.B) { benchTap(b, ollamaLine, false, true) })
}

func BenchmarkTapOpenAIChunk(b *testing.B) {
	b.Run("plain", func(b *testing.B) { benchTap(b, openaiLine, true, false) })
	b.Run("live", func(b *testing.B) { benchTap(b, openaiLine, true, true) })
}

func TestRateSkipsFirstToken(t *testing.T) {
	for _, c := range []struct {
		out  int
		want float64
	}{{2, 1}, {10, 9}, {1, 0.5}} {
		tp := &tap{first: time.Now().Add(-time.Second)}
		got := tp.rate(c.out, 2*time.Second)
		if got < c.want*0.95 || got > c.want*1.05 {
			t.Errorf("out=%d: got %.2f, want ~%.2f", c.out, got, c.want)
		}
	}
}

func TestLiveRowOnlyForPOST(t *testing.T) {
	for _, m := range []string{http.MethodOptions, http.MethodGet} {
		up, ch := gated(t, "text/event-stream")
		store := NewStore(10)
		front := proxyFor(t, up.URL, store)
		req, _ := http.NewRequest(m, front.URL+"/v1/chat/completions", nil)
		done := make(chan struct{})
		go func() {
			defer close(done)
			if resp, err := http.DefaultClient.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		time.Sleep(150 * time.Millisecond)
		if n := len(store.Flights()); n != 0 {
			t.Errorf("%s created %d live rows", m, n)
		}
		close(ch)
		<-done
	}
}
