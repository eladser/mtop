package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestByModelTiesSortByName(t *testing.T) {
	reqs := []Request{{Model: "b", TokSec: 1}, {Model: "a", TokSec: 1}, {Model: "c", TokSec: 1}}
	for i := 0; i < 20; i++ {
		got := byModel(reqs)
		if got[0].Model != "a" || got[1].Model != "b" || got[2].Model != "c" {
			t.Fatalf("want a,b,c got %v", got)
		}
	}
}

func TestRequestJSONOmitsCapturedText(t *testing.T) {
	b, err := json.Marshal(Request{Model: "m", Prompt: "secret p", Completion: "secret c", Thinking: "secret t"})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"Prompt"`, `"Completion"`, `"Thinking"`, "secret"} {
		if strings.Contains(string(b), k) {
			t.Fatalf("%q leaked into %s", k, b)
		}
	}
	var r Request
	old := `{"Model":"m","Prompt":"p","Completion":"c","Thinking":"t","OutTk":3}`
	if err := json.Unmarshal([]byte(old), &r); err != nil {
		t.Fatal(err)
	}
	if r.Model != "m" || r.OutTk != 3 || r.Prompt != "" {
		t.Fatalf("bad decode: %+v", r)
	}
}
