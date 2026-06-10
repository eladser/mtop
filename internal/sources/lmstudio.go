package sources

// LM Studio's REST api (v0 as of this writing) lists models with a
// state field; "loaded" is what we're after.
func (s *Scanner) scanLMStudio() ([]Row, bool) {
	if s.lmstudio == "" {
		return nil, false
	}
	var resp struct {
		Data []struct {
			ID           string `json:"id"`
			State        string `json:"state"`
			Quantization string `json:"quantization"`
			MaxContext   int    `json:"max_context_length"`
		} `json:"data"`
	}
	if s.getJSON(s.lmstudio+"/api/v0/models", &resp) != nil {
		return nil, false
	}
	var rows []Row
	for _, m := range resp.Data {
		if m.State != "loaded" {
			continue
		}
		rows = append(rows, Row{Name: m.ID, Quant: m.Quantization, From: "lm studio"})
	}
	return rows, true
}
