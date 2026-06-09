package gpu

import "testing"

func TestParse(t *testing.T) {
	out := "NVIDIA GeForce RTX 4080, 35, 8123, 16376, 62, 180.25\n"
	all := parse(out)
	if len(all) != 1 {
		t.Fatalf("expected 1 gpu, got %d", len(all))
	}
	g := all[0]
	if g.Name != "NVIDIA GeForce RTX 4080" || g.Util != 35 || g.MemUsed != 8123 ||
		g.MemTotal != 16376 || g.Temp != 62 || g.Power != 180.25 {
		t.Fatalf("bad parse: %+v", g)
	}
}

func TestParseGarbage(t *testing.T) {
	if all := parse("not, csv\n"); len(all) != 0 {
		t.Fatalf("garbage should parse to nothing, got %+v", all)
	}
}
