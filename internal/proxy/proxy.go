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
	"strings"
	"time"
)

// give up looking for the final chunk past this much buffered body; a
// response that big loses its metrics, which beats holding it all in
// memory if a server misbehaves
const maxBuf = 1 << 20

// error bodies are small; 64KB is plenty to catch a context-length
// message without holding a misbehaving server's output in memory
const maxErrBuf = 64 * 1024

type Proxy struct {
	target  *url.URL
	store   *Store
	host    string // listen hostname, allowed alongside loopback
	inspect bool   // capture prompt/completion text too
}

func New(upstream string, store *Store, inspect bool) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	return &Proxy{target: u, store: store, inspect: inspect}, nil
}

func (p *Proxy) Handler() http.Handler {
	rp := httputil.NewSingleHostReverseProxy(p.target)
	rp.Transport = &tapTransport{store: p.store, inspect: p.inspect}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		io.WriteString(w, p.store.PromText())
	})
	mux.Handle("/", rp)
	return p.guard(mux)
}

// guard keeps a web page from driving the local ollama api behind your
// back. The proxy forwards everything to ollama, including destructive
// endpoints like /api/delete, so a page you happen to have open could
// otherwise POST to 127.0.0.1:4321 (the browser sits on your loopback
// too) or rebind dns to read your model list. CLI clients and SDKs
// don't send Origin and call with a loopback Host, so they pass.
func (p *Proxy) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// an empty Host can't come from a browser (they always send one),
		// so it's some plain client; let it through
		if r.Host != "" && !p.ok(hostname(r.Host)) {
			http.Error(w, "blocked: unexpected host", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || !p.ok(u.Hostname()) {
				http.Error(w, "blocked: cross-origin request", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ok allows loopback and whatever host the proxy was told to listen on
// (so an intentional lan bind still works for its own address).
func (p *Proxy) ok(host string) bool {
	if host == "localhost" || host == p.host {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

// tapTransport wraps the response body of generation endpoints so the
// chunks can be read as they stream through.
type tapTransport struct {
	store   *Store
	inspect bool
}

func (t *tapTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// keep bodies plain so we can read the chunks
	req.Header.Del("Accept-Encoding")
	// before the round trip, so openai wall-time includes prompt processing
	started := time.Now()

	path := req.URL.Path
	ollama := path == "/api/generate" || path == "/api/chat"
	openai := path == "/v1/chat/completions" || path == "/v1/completions"
	responses := path == "/v1/responses"

	var prompt, reqModel string
	if (ollama || openai) && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		reqModel = modelOf(body)
		if t.inspect {
			prompt = promptOf(body)
		}
	}

	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// a 4xx on a generation path is worth a look: it's how llama.cpp,
	// vLLM, SGLang and ollama (with truncate:false) report a prompt that
	// blew the context window
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && (ollama || openai || responses) {
		resp.Body = &errTap{rc: resp.Body, store: t.store, model: reqModel}
		return resp, nil
	}
	if ollama || openai || responses {
		resp.Body = &tap{rc: resp.Body, store: t.store, path: path, openai: openai, responses: responses,
			started: started, inspect: t.inspect, prompt: prompt}
	}
	return resp, nil
}

// modelOf pulls the model name out of a request body, ollama or openai
// shape. Best effort, used to label a rejected request since the error
// body itself rarely names the model.
func modelOf(body []byte) string {
	var b struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &b)
	return b.Model
}

// promptOf digs the user's text out of a request body, ollama or openai
// shape, for the inspector. Best effort.
func promptOf(body []byte) string {
	var b struct {
		Prompt   string `json:"prompt"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &b) != nil {
		return ""
	}
	if b.Prompt != "" {
		return clip(b.Prompt)
	}
	for i := len(b.Messages) - 1; i >= 0; i-- {
		if b.Messages[i].Role == "user" {
			return clip(b.Messages[i].Content)
		}
	}
	return ""
}

// clip strips terminal control bytes and caps length by rune. The
// inspector prints captured prompts and completions to the terminal, so
// a model could otherwise smuggle escape sequences (clipboard writes,
// title spoofing) through its output. Drop them at capture. Keep \n and
// \t since the inspector wraps on newlines. 2 KB is plenty to eyeball.
func clip(s string) string {
	const max = 2048
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == '\n' || r == '\t' || (r >= 0x20 && r != 0x7f && !(r >= 0x80 && r <= 0x9f)) {
			b.WriteRune(r)
			if n++; n >= max {
				b.WriteString("…")
				break
			}
		}
	}
	return b.String()
}

func (p *Proxy) Listen(addr string) error {
	p.host = hostname(addr)
	// prompts go through this port as plain http, so binding anything
	// beyond loopback deserves a heads-up
	if p.host != "localhost" {
		ip := net.ParseIP(p.host)
		if p.host == "" || p.host == "0.0.0.0" || p.host == "::" || (ip != nil && !ip.IsLoopback()) {
			fmt.Fprintln(os.Stderr, "warning: proxy is reachable from the network, with no tls and no auth")
		}
	}
	return http.ListenAndServe(addr, p.Handler())
}

// chunk is the slice of an ollama response we care about. The final
// chunk (done:true) carries the counters; durations are nanoseconds.
type chunk struct {
	Model    string `json:"model"`
	Done     bool   `json:"done"`
	Response string `json:"response"` // /api/generate
	Message  struct {
		Content string `json:"content"`
	} `json:"message"` // /api/chat
	PromptEvalCount    int   `json:"prompt_eval_count"`
	EvalCount          int   `json:"eval_count"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalDuration       int64 `json:"eval_duration"`
	TotalDuration      int64 `json:"total_duration"`
}

// tap reads through a (possibly streaming) body, recording the final
// chunk without buffering or delaying anything the client sees.
type tap struct {
	rc        io.ReadCloser
	store     *Store
	path      string
	openai    bool
	responses bool
	started   time.Time
	inspect   bool
	prompt    string
	comp      strings.Builder
	buf       bytes.Buffer
	done      bool

	// openai streaming without a usage block: llama.cpp's own timings on
	// the last chunk are exact, otherwise tokens are estimated from the
	// number of content-bearing delta chunks seen.
	oModel   string
	oTimings *oaiTimings
	oDeltas  int
}

type oaiTimings struct {
	PromptN            int     `json:"prompt_n"`
	CacheN             int     `json:"cache_n"` // reused from context shifting; not part of prompt_n
	PredictedN         int     `json:"predicted_n"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
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
	switch {
	case t.openai:
		t.recordOpenAI(line)
		return
	case t.responses:
		t.recordResponses(line)
		return
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}
	var c chunk
	if json.Unmarshal(line, &c) != nil {
		return
	}
	if t.inspect && t.comp.Len() < 2048 {
		t.comp.WriteString(c.Response)
		t.comp.WriteString(c.Message.Content)
	}
	if !c.Done {
		return
	}
	t.done = true
	r := Request{
		When:       time.Now(),
		Path:       t.path,
		Model:      c.Model,
		PromptTk:   c.PromptEvalCount,
		OutTk:      c.EvalCount,
		Ctx:        t.store.CtxFor(c.Model),
		Total:      time.Duration(c.TotalDuration),
		Load:       time.Duration(c.LoadDuration),
		PromptEval: time.Duration(c.PromptEvalDuration),
	}
	if c.EvalDuration > 0 {
		r.TokSec = float64(c.EvalCount) / (float64(c.EvalDuration) / 1e9)
	}
	if t.inspect {
		r.Prompt = t.prompt
		r.Completion = clip(t.comp.String())
	}
	t.store.Add(r)
}

// OpenAI-style responses (llama.cpp, LM Studio, vLLM all speak this) put
// counters in a usage block: on the last SSE chunk when streaming, or on
// the body itself otherwise. mtop doesn't inject stream_options to ask
// for that block on streams that don't already send one, since clients
// are promised the exact same bytes back, so a plain SSE stream (no
// usage) falls through to finishOpenAI once it ends.
func (t *tap) recordOpenAI(line []byte) {
	line = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(line) == 0 {
		return
	}
	if bytes.Equal(line, []byte("[DONE]")) {
		t.finishOpenAI()
		return
	}
	var c struct {
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Timings *oaiTimings `json:"timings"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(line, &c) != nil {
		return
	}
	if c.Model != "" {
		t.oModel = c.Model
	}
	if c.Timings != nil {
		t.oTimings = c.Timings
	}
	for _, ch := range c.Choices {
		if ch.Delta.Content != "" {
			t.oDeltas++
		}
	}
	if c.Usage == nil || c.Usage.CompletionTokens == 0 {
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
		Ctx:      t.store.CtxFor(c.Model),
		Total:    wall,
	}
	if s := wall.Seconds(); s > 0 {
		r.TokSec = float64(c.Usage.CompletionTokens) / s
	}
	t.store.Add(r)
}

// finishOpenAI records a streamed request that never got a usage block.
// llama.cpp's own timings (predicted_n, predicted_per_second) are exact
// when present; otherwise the delta chunk count stands in for output
// tokens, which is close enough for most local servers that emit one
// token per chunk.
func (t *tap) finishOpenAI() {
	if t.done {
		return
	}
	t.done = true
	wall := time.Since(t.started)
	r := Request{When: time.Now(), Path: t.path, Model: t.oModel, Ctx: t.store.CtxFor(t.oModel), Total: wall}
	switch {
	case t.oTimings != nil && t.oTimings.PredictedN > 0:
		// prompt_n is the freshly-processed part; cache_n is what the
		// server already had cached from earlier in the conversation
		r.PromptTk = t.oTimings.PromptN + t.oTimings.CacheN
		r.OutTk = t.oTimings.PredictedN
		r.TokSec = t.oTimings.PredictedPerSecond
	case t.oDeltas > 0:
		r.OutTk = t.oDeltas
		r.Estimated = true
		if s := wall.Seconds(); s > 0 {
			r.TokSec = float64(t.oDeltas) / s
		}
	default:
		return
	}
	t.store.Add(r)
}

// /v1/responses (llama.cpp's OpenAI Responses API passthrough) carries
// usage on the response object: at the top level for a plain body, or
// nested under "response" on the streamed response.completed event.
func (t *tap) recordResponses(line []byte) {
	line = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(line), []byte("data:")))
	if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
		return
	}
	type usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	}
	var c struct {
		Model    string `json:"model"`
		Usage    *usage `json:"usage"`
		Response *struct {
			Model string `json:"model"`
			Usage *usage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &c) != nil {
		return
	}
	model, u := c.Model, c.Usage
	if c.Response != nil {
		if c.Response.Model != "" {
			model = c.Response.Model
		}
		if c.Response.Usage != nil {
			u = c.Response.Usage
		}
	}
	if u == nil || u.OutputTokens == 0 {
		return
	}
	t.done = true
	wall := time.Since(t.started)
	r := Request{
		When:     time.Now(),
		Path:     t.path,
		Model:    model,
		PromptTk: u.InputTokens,
		OutTk:    u.OutputTokens,
		Ctx:      t.store.CtxFor(model),
		Total:    wall,
	}
	if s := wall.Seconds(); s > 0 {
		r.TokSec = float64(u.OutputTokens) / s
	}
	t.store.Add(r)
}

func (t *tap) Close() error {
	if !t.done && t.buf.Len() < maxBuf {
		t.record(t.buf.Bytes())
	}
	if t.openai && !t.done {
		t.finishOpenAI()
	}
	return t.rc.Close()
}

// errTap watches a 4xx body go by looking for a context-length rejection.
// Same rule as tap: the client gets every byte untouched, this only ever
// reads a copy.
type errTap struct {
	rc    io.ReadCloser
	store *Store
	model string
	buf   bytes.Buffer
	done  bool
}

func (e *errTap) Read(b []byte) (int, error) {
	n, err := e.rc.Read(b)
	if n > 0 && !e.done && e.buf.Len() < maxErrBuf {
		e.buf.Write(b[:n])
	}
	if err == io.EOF {
		e.finish()
	}
	return n, err
}

func (e *errTap) Close() error {
	e.finish()
	return e.rc.Close()
}

func (e *errTap) finish() {
	if e.done {
		return
	}
	e.done = true
	ctx, promptTk, ok := detectOverflow(e.buf.Bytes())
	if !ok {
		return
	}
	if ctx == 0 {
		ctx = e.store.CtxFor(e.model)
	}
	e.store.Add(Request{When: time.Now(), Model: e.model, Overflow: true, PromptTk: promptTk, Ctx: ctx})
}

// detectOverflow looks for the handful of ways a server says "no, that
// prompt doesn't fit". llama.cpp's own error carries exact numbers; vLLM
// and SGLang just say so in the message; ollama (with truncate:false)
// says so too, though without any numbers attached.
func detectOverflow(body []byte) (ctx, promptTk int, ok bool) {
	var lcpp struct {
		Error struct {
			Type          string `json:"type"`
			NPromptTokens int    `json:"n_prompt_tokens"`
			NCtx          int    `json:"n_ctx"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &lcpp) == nil && lcpp.Error.Type == "exceed_context_size_error" {
		return lcpp.Error.NCtx, lcpp.Error.NPromptTokens, true
	}
	s := string(body)
	switch {
	case strings.Contains(s, "maximum context length is"), // vLLM
		strings.Contains(s, "longer than the model's context length"),      // SGLang
		strings.Contains(s, "the input length exceeds the context length"): // ollama, truncate:false
		return 0, 0, true
	}
	return 0, 0, false
}
