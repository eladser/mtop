// Package proxy sits between any client and the inference server.
// Ollama has no metrics endpoint; the only place per-request numbers
// exist is the response stream itself, where the final chunk carries
// token counts and timings. So we forward traffic untouched and read
// the chunks as they pass through. OpenAI-style endpoints get the
// same treatment using their usage block.
package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

// give up looking for the final chunk past this much buffered body; a
// response that big loses its metrics, which beats holding it all in
// memory if a server misbehaves
const maxBuf = 1 << 20

type Proxy struct {
	target *url.URL
	store  *Store
}

func New(upstream string, store *Store) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	return &Proxy{target: u, store: store}, nil
}

func (p *Proxy) Handler() http.Handler {
	rp := httputil.NewSingleHostReverseProxy(p.target)
	rp.Transport = &tapTransport{store: p.store}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		io.WriteString(w, p.store.PromText())
	})
	mux.Handle("/", rp)
	return mux
}

// tapTransport wraps the response body of generation endpoints so the
// chunks can be read as they stream through.
type tapTransport struct {
	store *Store
}

func (t *tapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// keep bodies plain so we can read the chunks
	req.Header.Del("Accept-Encoding")
	// before the round trip, so openai wall-time includes prompt processing
	started := time.Now()

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	path := req.URL.Path
	ollama := path == "/api/generate" || path == "/api/chat"
	openai := path == "/v1/chat/completions" || path == "/v1/completions"
	if ollama || openai {
		resp.Body = &tap{rc: resp.Body, store: t.store, path: path, openai: openai, started: started}
	}
	return resp, nil
}

func (p *Proxy) Listen(addr string) error {
	// prompts go through this port as plain http, so binding anything
	// beyond loopback deserves a heads-up
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "localhost" {
		ip := net.ParseIP(host)
		if host == "" || host == "0.0.0.0" || host == "::" || (ip != nil && !ip.IsLoopback()) {
			fmt.Fprintln(os.Stderr, "warning: proxy is reachable from the network, with no tls and no auth")
		}
	}
	return http.ListenAndServe(addr, p.Handler())
}

// chunk is the slice of an ollama response we care about. The final
// chunk (done:true) carries the counters; durations are nanoseconds.
type chunk struct {
	Model           string `json:"model"`
	Done            bool   `json:"done"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	EvalDuration    int64  `json:"eval_duration"`
	TotalDuration   int64  `json:"total_duration"`
}

// tap reads through a (possibly streaming) body, recording the final
// chunk without buffering or delaying anything the client sees.
type tap struct {
	rc      io.ReadCloser
	store   *Store
	path    string
	openai  bool
	started time.Time
	buf     bytes.Buffer
	done    bool
}

func (t *tap) Read(b []byte) (int, error) {
	n, err := t.rc.Read(b)
	if n > 0 && !t.done && t.buf.Len() < maxBuf {
		t.buf.Write(b[:n])
		t.drain()
	}
	if err == io.EOF && !t.done {
		// non-streaming bodies may end without a trailing newline
		t.record(t.buf.Bytes())
	}
	return n, err
}

func (t *tap) drain() {
	for {
		line, err := t.buf.ReadBytes('\n')
		if err != nil {
			// partial line; buffer is empty now, so this re-queues it
			t.buf.Write(line)
			return
		}
		t.record(line)
	}
}

func (t *tap) record(line []byte) {
	if t.openai {
		t.recordOpenAI(line)
		return
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var c chunk
	if json.Unmarshal(line, &c) != nil || !c.Done {
		return
	}
	t.done = true
	r := Request{
		When:     time.Now(),
		Path:     t.path,
		Model:    c.Model,
		PromptTk: c.PromptEvalCount,
		OutTk:    c.EvalCount,
		Total:    time.Duration(c.TotalDuration),
	}
	if c.EvalDuration > 0 {
		r.TokSec = float64(c.EvalCount) / (float64(c.EvalDuration) / 1e9)
	}
	t.store.Add(r)
}

// OpenAI-style responses (llama.cpp, LM Studio, vLLM all speak this)
// put counters in a usage block: on the last SSE chunk when streaming,
// or on the body itself otherwise. No timings though, so tok/s here is
// wall-clock and includes prompt processing.
func (t *tap) recordOpenAI(line []byte) {
	line = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
		return
	}
	var c struct {
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(line, &c) != nil || c.Usage == nil || c.Usage.CompletionTokens == 0 {
		return
	}
	t.done = true
	wall := time.Since(t.started)
	r := Request{
		When:     time.Now(),
		Path:     t.path,
		Model:    c.Model,
		PromptTk: c.Usage.PromptTokens,
		OutTk:    c.Usage.CompletionTokens,
		Total:    wall,
	}
	if s := wall.Seconds(); s > 0 {
		r.TokSec = float64(c.Usage.CompletionTokens) / s
	}
	t.store.Add(r)
}

func (t *tap) Close() error {
	if !t.done {
		t.record(t.buf.Bytes())
	}
	return t.rc.Close()
}
