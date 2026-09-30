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
	var rows []Row
	for _, m := range resp.AllModelsLoaded {
		m := m
		note := m.Device
		if ctx, ok := m.RecipeOptions["ctx_size"].(float64); ok && ctx > 0 {
			note = fmt.Sprintf("%s · ctx %d", m.Device, int(ctx))
		}
		rows = append(rows, Row{
			Name:   m.ModelName,
			Note:   note,
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
