package sources

import (
	"fmt"
	"net/url"
	"path/filepath"
)

// llama.cpp server: /props has the loaded model, /metrics (needs
// --metrics on launch) has how many requests are in flight and queued
// right now, draft acceptance with a draft model, plus kv-cache use on
// builds that still report it.
//
// Only /health, /props, /models and /metrics are sleep-exempt. Anything
// else (/slots included) wakes a sleeping server and resets its idle
// timer, so mtop never touches it.
func (s *Scanner) scanLlamacpp() ([]Row, bool) {
	if s.llamacpp == "" {
		return nil, false
	}
	var props struct {
		Role       string `json:"role"`
		ModelPath  string `json:"model_path"`
		ModelAlias string `json:"model_alias"`
		TotalSlots int    `json:"total_slots"`
		Settings   struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if s.getJSON(s.llamacpp+"/props", &props) != nil {
		return nil, false
	}
	// router mode (llama-server started without -m) has no single model:
	// /props says role "router" and model_path "none" (older builds: empty)
	if props.Role == "router" || props.ModelPath == "" || props.ModelPath == "none" {
		if rows, ok := s.scanLlamacppRouter(); ok {
			return rows, true
		}
	}
	name := filepath.Base(props.ModelPath)
	if name == "." || name == "/" {
		name = "(model)"
	}
	row := Row{Name: name, From: "llama.cpp", Ctx: props.Settings.NCtx, Slots: props.TotalSlots, Alias: props.ModelAlias}
	s.llamacppMetrics(&row, s.llamacpp+"/metrics")
	if props.Settings.NCtx > 0 {
		row.Size = fmt.Sprintf("%dk ctx", props.Settings.NCtx/1024)
	}
	return []Row{row}, true
}

// llamacppMetrics fills Note, Busy and Queued from one /metrics scrape.
func (s *Scanner) llamacppMetrics(row *Row, u string) bool {
	m, err := s.getProm(u)
	if err != nil {
		return false
	}
	row.Busy = int(m["llamacpp:requests_processing"])
	note := fmt.Sprintf("%d running", row.Busy)
	if v, ok := m["llamacpp:requests_deferred"]; ok && v > 0 {
		row.Queued = int(v)
		note += fmt.Sprintf(" · %d queued", row.Queued)
	}
	// spec totals count from server start, so this is the lifetime rate.
	// predicted_tokens_seconds stays unread, it reads 0 while a request is still generating
	if d, ok := m["llamacpp:spec_decode_num_draft_tokens_total"]; ok && d > 0 {
		note += fmt.Sprintf(" · draft %.0f%%", m["llamacpp:spec_decode_num_accepted_tokens_total"]/d*100)
	}
	// current llama.cpp dropped the kv gauge; missing isn't 0%
	if v, ok := m["llamacpp:kv_cache_usage_ratio"]; ok {
		note = fmt.Sprintf("kv %.0f%% · %s", v*100, note)
	}
	row.Note = note
	return true
}

// routerMetrics is llamacppMetrics for one router child, which can be
// slow to answer while it is busy. The last good reading stays up until
// three scrapes in a row fail, so the note does not blink empty.
func (s *Scanner) routerMetrics(row *Row, id, u string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.probes == nil {
		s.probes = map[string]*probe{}
	}
	p := s.probes[id]
	if p == nil {
		p = &probe{}
		s.probes[id] = p
	}
	if s.llamacppMetrics(row, u) {
		p.note, p.busy, p.queued, p.fails = row.Note, row.Busy, row.Queued, 0
		return
	}
	if p.fails++; p.fails < 3 {
		row.Note, row.Busy, row.Queued = p.note, p.busy, p.queued
	}
}

// scanLlamacppRouter lists models from /models, which the router exposes
// with a status per model instead of /props' single model_path. Loaded
// models get their own /props and /metrics probe; the router proxies
// those with ?model=, and autoload=false keeps a probe from loading
// anything. Unloaded models are never probed.
func (s *Scanner) scanLlamacppRouter() ([]Row, bool) {
	var resp struct {
		Data []struct {
			ID      string   `json:"id"`
			Path    string   `json:"path"`
			Aliases []string `json:"aliases"`
			Status  struct {
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
		row := Row{Name: name, From: "llama.cpp", Aliases: append(append([]string{}, m.Aliases...), m.ID)}
		q := "?model=" + url.QueryEscape(m.ID) + "&autoload=false"
		var props struct {
			ModelAlias string `json:"model_alias"`
			TotalSlots int    `json:"total_slots"`
			Settings   struct {
				NCtx int `json:"n_ctx"`
			} `json:"default_generation_settings"`
		}
		if s.getJSON(s.llamacpp+"/props"+q, &props) == nil {
			row.Slots, row.Alias, row.Ctx = props.TotalSlots, props.ModelAlias, props.Settings.NCtx
			if row.Ctx > 0 {
				row.Size = fmt.Sprintf("%dk ctx", row.Ctx/1024)
			}
		}
		s.routerMetrics(&row, m.ID, s.llamacpp+"/metrics"+q)
		rows = append(rows, row)
	}
	// router is up even when nothing is loaded yet; don't fall back to a phantom row
	return rows, true
}
