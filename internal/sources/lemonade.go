package sources

import "fmt"

// Lemonade's /v1/health doubles as its "what's loaded" endpoint; each
// entry carries the device split (cpu/gpu/npu) and, for llamacpp-backed
// recipes, the context size it was loaded with.
func (s *Scanner) scanLemonade() ([]Row, bool) {
	if s.lemonade == "" {
		return nil, false
	}
	var resp struct {
		AllModelsLoaded []struct {
			ModelName     string         `json:"model_name"`
			Device        string         `json:"device"`
			RecipeOptions map[string]any `json:"recipe_options"`
		} `json:"all_models_loaded"`
	}
	if s.getJSON(s.lemonade+"/v1/health", &resp) != nil {
		return nil, false
	}
	// /v1/stats has live numbers since v2026.43.3, but they're server-wide,
	// so they only go on the row when there's a single model to pin them
	// on. Older builds 404 or leave out "live" and the note stays as is.
	running, tps := 0, 0.0
	if len(resp.AllModelsLoaded) == 1 {
		var st struct {
			Live struct {
				Active int     `json:"active_requests"`
				TPS    float64 `json:"estimated_tokens_per_second"`
			} `json:"live"`
		}
		if s.getJSON(s.lemonade+"/v1/stats", &st) == nil {
			running, tps = st.Live.Active, st.Live.TPS
		}
	}
	var rows []Row
	for _, m := range resp.AllModelsLoaded {
		m := m
		note := m.Device
		ctx := 0
		if c, ok := m.RecipeOptions["ctx_size"].(float64); ok && c > 0 {
			ctx = int(c)
			note = fmt.Sprintf("%s · ctx %d", m.Device, ctx)
		}
		if running > 0 {
			note += fmt.Sprintf(" · %d running", running)
			if tps > 0 {
				note += fmt.Sprintf(" · %.0f tok/s", tps)
			}
		}
		cpu := 0
		if m.Device == "cpu" {
			cpu = 100
		}
		rows = append(rows, Row{
			Name:   m.ModelName,
			Note:   note,
			Ctx:    ctx,
			CPU:    cpu,
			From:   "lemonade",
			Unload: func() error { return s.unloadLemonade(m.ModelName) },
		})
	}
	return rows, true
}

func (s *Scanner) unloadLemonade(model string) error {
	return s.postJSON(s.lemonade+"/v1/unload", struct {
		ModelName string `json:"model_name"`
	}{model})
}
