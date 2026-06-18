package ollama

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base string
	hc   *http.Client
}

func New(base string) *Client {
	return &Client{base: base, hc: &http.Client{Timeout: 3 * time.Second}}
}

// Host is the bare host:port of the server, for labelling rows when
// more than one ollama is being watched.
func (c *Client) Host() string {
	s := c.base
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	return strings.TrimSuffix(s, "/")
}

type Model struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SizeVRAM  int64     `json:"size_vram"`
	ExpiresAt time.Time `json:"expires_at"`
	Details   Details   `json:"details"`
}

type Details struct {
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
}

type listResponse struct {
	Models []Model `json:"models"`
}

// Loaded returns models currently in memory (what `ollama ps` shows).
func (c *Client) Loaded() ([]Model, error) {
	var r listResponse
	if err := c.get("/api/ps", &r); err != nil {
		return nil, err
	}
	return r.Models, nil
}

// OnDisk returns how many models are pulled locally.
func (c *Client) OnDisk() (int, error) {
	var r listResponse
	if err := c.get("/api/tags", &r); err != nil {
		return 0, err
	}
	return len(r.Models), nil
}

// Unload drops a model from memory now. There's no dedicated endpoint
// for this; a generate call with keep_alive 0 and no prompt is how
// `ollama stop` does it too.
func (c *Client) Unload(model string) error {
	body := fmt.Sprintf(`{"model":%q,"keep_alive":0}`, model)
	resp, err := c.hc.Post(c.base+"/api/generate", "application/json", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: %s", resp.Status)
	}
	return nil
}

// Overdue reports a model that should have expired but is still loaded.
// Happens more than it should; this is what the u key is for.
func (m Model) Overdue() bool {
	return !m.ExpiresAt.IsZero() && time.Now().After(m.ExpiresAt)
}

func (c *Client) get(path string, v any) error {
	resp, err := c.hc.Get(c.base + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
