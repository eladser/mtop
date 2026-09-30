// Package sources finds models on whatever local AI servers happen to
// be running. Ollama is the main one; llama.cpp, LM Studio and vLLM
// get probed on their usual ports and merged into the same list.
package sources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/eladser/mtop/internal/ollama"
)

type Row struct {
	Name    string
	Size    string
	Quant   string
	VRAM    int64
	CPU     int // percent of the model sitting in system RAM instead of VRAM, 0 if unknown
	Ctx     int // context length, 0 if unknown
	Expires time.Time
	From    string
	Note    string       // extra per-server detail, e.g. kv-cache use
	Unload  func() error // nil when the server has no way to
}

type Scanner struct {
	olls      []*ollama.Client
	llamacpp  string
	lmstudio  string
	vllm      string
	llamaswap string
	lemonade  string
	sglang    string
	hc        *http.Client
}

func New(olls []*ollama.Client, llamacpp, lmstudio, vllm, llamaswap, lemonade, sglang string) *Scanner {
	return &Scanner{
		olls:      olls,
		llamacpp:  llamacpp,
		lmstudio:  lmstudio,
		vllm:      vllm,
		llamaswap: llamaswap,
		lemonade:  lemonade,
		sglang:    sglang,
		hc:        &http.Client{Timeout: 800 * time.Millisecond},
	}
}

// Scan returns every loaded model it can find, plus which servers
// answered. ollErr is ollama's error specifically, since that's the
// one worth telling the user about.
func (s *Scanner) Scan() (rows []Row, alive []string, ollErr error) {
	multi := len(s.olls) > 1
	ollAlive := false
	for _, oll := range s.olls {
		oll := oll
		models, err := oll.Loaded()
		if err != nil {
			ollErr = err
			continue
		}
		ollAlive = true
		from := "ollama"
		if multi {
			from = "ollama@" + oll.Host()
		}
		for _, m := range models {
			m := m
			cpu := 0
			if m.Size > 0 {
				cpu = int((m.Size - m.SizeVRAM) * 100 / m.Size)
			}
			rows = append(rows, Row{
				Name:    m.Name,
				Size:    m.Details.ParameterSize,
				Quant:   m.Details.QuantizationLevel,
				VRAM:    m.SizeVRAM,
				CPU:     cpu,
				Ctx:     m.ContextLength,
				Expires: m.ExpiresAt,
				From:    from,
				Unload:  func() error { return oll.Unload(m.Name) },
			})
		}
	}
	if ollAlive {
		alive = append(alive, "ollama")
		ollErr = nil // at least one answered
	}
	// llama-swap defaults to the same :8080 port as bare llama.cpp and
	// falls back to -llamacpp when -llamaswap isn't set; when it answers
	// there, scanLlamacpp would be talking to the same server (it proxies
	// /props) and produce a duplicate or phantom row, so skip it.
	swapRows, swapOK := s.scanLlamaswap()
	swapOnLlamacppURL := swapOK && s.llamaswap == "" && s.llamacpp != ""

	if !swapOnLlamacppURL {
		if r, ok := s.scanLlamacpp(); ok {
			alive = append(alive, "llama.cpp")
			rows = append(rows, r...)
		}
	}
	if r, ok := s.scanLMStudio(); ok {
		alive = append(alive, "lm studio")
		rows = append(rows, r...)
	}
	if r, ok := s.scanVllm(); ok {
		alive = append(alive, "vllm")
		rows = append(rows, r...)
	}
	if swapOK {
		alive = append(alive, "llama-swap")
		rows = append(rows, swapRows...)
	}
	if r, ok := s.scanLemonade(); ok {
		alive = append(alive, "lemonade")
		rows = append(rows, r...)
	}
	if r, ok := s.scanSglang(); ok {
		alive = append(alive, "sglang")
		rows = append(rows, r...)
	}
	return rows, alive, ollErr
}

// OnDisk is how many models ollama has pulled locally, summed across
// hosts. Cosmetic, so errors just mean zero.
func (s *Scanner) OnDisk() int {
	total := 0
	for _, oll := range s.olls {
		n, _ := oll.OnDisk()
		total += n
	}
	return total
}

func (s *Scanner) getJSON(url string, v any) error {
	resp, err := s.hc.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (s *Scanner) postJSON(url string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := s.hc.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}
