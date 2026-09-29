package sources

import (
	"fmt"
	"path/filepath"
)

// llama.cpp server: /props has the loaded model, /metrics (needs
// --metrics on launch) has kv-cache use and how many requests are in
// flight right now.
func (s *Scanner) scanLlamacpp() ([]Row, bool) {
	if s.llamacpp == "" {
		return nil, false
	}
	var props struct {
		ModelPath string `json:"model_path"`
		Settings  struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if s.getJSON(s.llamacpp+"/props", &props) != nil {
		return nil, false
	}
	// router mode (llama-server started without -m) has no single model,
	// so /props comes back with an empty path; list what's loaded instead
	if props.ModelPath == "" {
		if rows, ok := s.scanLlamacppRouter(); ok {
			return rows, true
		}
	}
	name := filepath.Base(props.ModelPath)
	if name == "." || name == "/" {
		name = "(model)"
	}
	note := ""
	if m, err := s.getProm(s.llamacpp + "/metrics"); err == nil {
		note = fmt.Sprintf("kv %.0f%% · %d running",
			m["llamacpp:kv_cache_usage_ratio"]*100, int(m["llamacpp:requests_processing"]))
	}
	row := Row{Name: name, From: "llama.cpp", Note: note}
	if props.Settings.NCtx > 0 {
		row.Size = fmt.Sprintf("%dk ctx", props.Settings.NCtx/1024)
	}
	return []Row{row}, true
}

// scanLlamacppRouter lists models from /models, which the router exposes
// with a status per model instead of /props' single model_path.
func (s *Scanner) scanLlamacppRouter() ([]Row, bool) {
	var resp struct {
		Data []struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Status struct {
				Value string `json:"value"`
			} `json:"status"`
		} `json:"data"`
	}
	if s.getJSON(s.llamacpp+"/models", &resp) != nil {
		return nil, false
	}
	var rows []Row
	for _, m := range resp.Data {
		if m.Status.Value != "loaded" {
			continue
		}
		name := filepath.Base(m.Path)
		if name == "." || name == "/" || name == "" {
			name = m.ID
		}
		rows = append(rows, Row{Name: name, From: "llama.cpp"})
	}
	// router is up even when nothing is loaded yet; don't fall back to a phantom row
	return rows, true
}
