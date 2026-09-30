package proxy

import "testing"

func TestByModelTiesSortByName(t *testing.T) {
	reqs := []Request{{Model: "b", TokSec: 1}, {Model: "a", TokSec: 1}, {Model: "c", TokSec: 1}}
	for i := 0; i < 20; i++ {
		got := byModel(reqs)
		if got[0].Model != "a" || got[1].Model != "b" || got[2].Model != "c" {
			t.Fatalf("want a,b,c got %v", got)
		}
	}
}
