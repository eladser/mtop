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

func TestParseAMD(t *testing.T) {
	out := []byte(`{"card0":{"Card series":"Radeon RX 7900 XTX","GPU use (%)":"12","Temperature (Sensor edge) (C)":"45.0","Average Graphics Package Power (W)":"63.0","VRAM Total Memory (B)":"25753026560","VRAM Total Used Memory (B)":"1073741824"},"system":{"Driver version":"6.3.2"}}`)
	all := parseAMD(out)
	if len(all) != 1 {
		t.Fatalf("expected 1 card, got %d", len(all))
	}
	g := all[0]
	if g.Name != "Radeon RX 7900 XTX" || g.Util != 12 || g.Temp != 45 || g.MemTotal != 24560 || g.MemUsed != 1024 {
		t.Fatalf("bad parse: %+v", g)
	}
}

func TestUsedFromVMStat(t *testing.T) {
	out := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 100.\nPages active: 1000.\nPages wired down: 500.\nPages occupied by compressor: 250.\n"
	// (1000+500+250) * 16384 / 1MiB = 27
	if got := usedFromVMStat(out); got != 27 {
		t.Fatalf("got %d MiB", got)
	}
}
