package gpu

import (
	"errors"
	"strings"
	"testing"
)

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
	all, err := parseAMD(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 card, got %d", len(all))
	}
	g := all[0]
	if g.Name != "Radeon RX 7900 XTX" || g.Util != 12 || g.Temp != 45 || g.MemTotal != 24560 || g.MemUsed != 1024 {
		t.Fatalf("bad parse: %+v", g)
	}
}

func TestParseAMDBadJSON(t *testing.T) {
	if _, err := parseAMD([]byte("not json")); err == nil {
		t.Fatal("expected an error on bad json")
	}
}

func TestResidencyOf(t *testing.T) {
	got, ok := residencyOf("GPU HW active residency:  42.50% (444 MHz: 12%)")
	if !ok || got != 43 {
		t.Fatalf("got %d ok=%v", got, ok)
	}
	if _, ok := residencyOf("GPU idle residency: 57.50%"); ok {
		t.Fatal("idle line should not match")
	}
	if _, ok := residencyOf("some other line"); ok {
		t.Fatal("unrelated line should not match")
	}
}

func TestUsedFromVMStat(t *testing.T) {
	out := "Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 100.\nPages active: 1000.\nPages wired down: 500.\nPages occupied by compressor: 250.\n"
	// (1000+500+250) * 16384 / 1MiB = 27
	if got := usedFromVMStat(out); got != 27 {
		t.Fatalf("got %d MiB", got)
	}
}

func TestParseThrottle(t *testing.T) {
	tests := []struct {
		name string
		line string
		want uint8
	}{
		{"none", "RTX 4090, 35, 100, 200, 60, 90.0, Not Active, Not Active, Not Active", 0},
		{"thermal", "RTX 4090, 35, 100, 200, 60, 90.0, Active, Not Active, Not Active", ThrottleThermal},
		{"power brake", "RTX 4090, 35, 100, 200, 60, 90.0, Not Active, Active, Not Active", ThrottlePower},
		{"hw", "RTX 4090, 35, 100, 200, 60, 90.0, Not Active, Not Active, Active", ThrottleHW},
		{"all", "RTX 4090, 35, 100, 200, 60, 90.0, Active, Active, Active", ThrottleThermal | ThrottlePower | ThrottleHW},
		{"n/a", "RTX 4090, 35, 100, 200, 60, 90.0, [N/A], [N/A], [N/A]", 0},
		{"not supported", "RTX 4090, 35, 100, 200, 60, 90.0, [Not Supported], [Not Supported], [Not Supported]", 0},
		{"legacy 6 fields", "RTX 4090, 35, 100, 200, 60, 90.0", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all := parse(tt.line + "\n")
			if len(all) != 1 || all[0].Throttle != tt.want {
				t.Fatalf("got %+v, want throttle %d", all, tt.want)
			}
			if all[0].Util != 35 || all[0].Power != 90 {
				t.Fatalf("base fields off: %+v", all[0])
			}
		})
	}
}

func TestNvidiaLadder(t *testing.T) {
	old := runSMI
	defer func() { runSMI = old }()
	const base = "RTX 4090, 35, 100, 200, 60, 90.0"
	run := func(accept string) (calls *[]string) {
		calls = new([]string)
		runSMI = func(path string, args ...string) ([]byte, error) {
			q := args[0]
			*calls = append(*calls, q)
			switch {
			case strings.Contains(q, "clocks_event_reasons") && accept == "event":
				return []byte(base + ", Active, Not Active, Not Active\n"), nil
			case strings.Contains(q, "clocks_throttle_reasons") && accept == "throttle":
				return []byte(base + ", Not Active, Active, Not Active\n"), nil
			case !strings.Contains(q, "clocks_") && accept == "base":
				return []byte(base + "\n"), nil
			}
			return nil, errFieldRejected
		}
		return
	}

	t.Run("event reasons", func(t *testing.T) {
		calls := run("event")
		var l smiLadder
		g, err := readNvidia(&l, "smi")
		if err != nil || len(g) != 1 || g[0].Throttle != ThrottleThermal {
			t.Fatalf("got %+v, %v", g, err)
		}
		readNvidia(&l, "smi")
		if len(*calls) != 2 {
			t.Fatalf("one call per read once decided, got %v", *calls)
		}
	})
	t.Run("old alias", func(t *testing.T) {
		calls := run("throttle")
		var l smiLadder
		g, err := readNvidia(&l, "smi")
		if err != nil || len(g) != 1 || g[0].Throttle != ThrottlePower {
			t.Fatalf("got %+v, %v", g, err)
		}
		n := len(*calls)
		readNvidia(&l, "smi")
		if len(*calls) != n+1 {
			t.Fatalf("should skip the failed rung next time: %v", *calls)
		}
	})
	t.Run("no throttle fields", func(t *testing.T) {
		calls := run("base")
		var l smiLadder
		g, err := readNvidia(&l, "smi")
		if err != nil || len(g) != 1 || g[0].Util != 35 || g[0].Throttle != 0 {
			t.Fatalf("pane must not blank: %+v, %v", g, err)
		}
		n := len(*calls)
		readNvidia(&l, "smi")
		if len(*calls) != n+1 {
			t.Fatalf("got %v", *calls)
		}
	})
	t.Run("everything fails", func(t *testing.T) {
		run("none")
		var l smiLadder
		if _, err := readNvidia(&l, "smi"); err == nil {
			t.Fatal("want the error")
		}
		if l.decided {
			t.Fatal("decided on a total failure")
		}
	})
}

func TestDebounce(t *testing.T) {
	var d Debounce
	hot := []Stats{{Util: 80, Throttle: ThrottleThermal}}
	for i := 1; i <= 2; i++ {
		if got := d.Update(hot); got[0] != 0 {
			t.Fatalf("poll %d showed early: %d", i, got[0])
		}
	}
	if got := d.Update(hot); got[0] != ThrottleThermal {
		t.Fatalf("poll 3 should show: %d", got[0])
	}
	cool := []Stats{{Util: 80}}
	for i := 1; i <= 2; i++ {
		if got := d.Update(cool); got[0] != ThrottleThermal {
			t.Fatalf("clean poll %d cleared early", i)
		}
	}
	if got := d.Update(cool); got[0] != 0 {
		t.Fatalf("3 clean polls should clear: %d", got[0])
	}
}

func TestDebounceGateAndFlicker(t *testing.T) {
	var d Debounce
	idle := []Stats{{Util: 29, Throttle: ThrottleHW}}
	for i := 0; i < 5; i++ {
		if got := d.Update(idle); got[0] != 0 {
			t.Fatal("showed under the util gate")
		}
	}
	on, off := []Stats{{Util: 90, Throttle: ThrottleHW}}, []Stats{{Util: 90}}
	d = Debounce{}
	d.Update(on)
	d.Update(on)
	d.Update(off) // flicker resets the count
	if got := d.Update(on); got[0] != 0 {
		t.Fatal("flicker counted")
	}
	// util dropping below the gate while shown counts as clean
	d = Debounce{}
	for i := 0; i < 3; i++ {
		d.Update(on)
	}
	for i := 0; i < 2; i++ {
		d.Update(idle)
	}
	if got := d.Update(idle); got[0] != 0 {
		t.Fatal("gate drop didn't clear")
	}
}

func TestDebounceBitsAndCount(t *testing.T) {
	var d Debounce
	both := []Stats{{Util: 90, Throttle: ThrottleThermal | ThrottleHW}, {Util: 90}}
	d.Update(both)
	d.Update(both)
	got := d.Update(both)
	if got[0] != ThrottleThermal|ThrottleHW || got[1] != 0 {
		t.Fatalf("got %v", got)
	}
	if got := d.Update(both[:1]); len(got) != 1 {
		t.Fatalf("gpu count change: %v", got)
	}
}

func TestNvidiaLadderTransientError(t *testing.T) {
	old := runSMI
	defer func() { runSMI = old }()
	calls, fail := 0, true
	runSMI = func(path string, args ...string) ([]byte, error) {
		calls++
		if fail {
			return nil, errors.New("signal: killed")
		}
		return []byte("RTX 4090, 35, 100, 200, 60, 90.0, Active, Not Active, Not Active\n"), nil
	}
	var l smiLadder
	if _, err := readNvidia(&l, "smi"); err == nil {
		t.Fatal("want the error")
	}
	if calls != 1 || l.decided {
		t.Fatalf("a timeout must stop the ladder undecided: calls=%d decided=%v", calls, l.decided)
	}
	fail = false
	g, err := readNvidia(&l, "smi")
	if err != nil || len(g) != 1 || g[0].Throttle != ThrottleThermal || l.step != 0 {
		t.Fatalf("next poll should land on the top rung: %+v %v step=%d", g, err, l.step)
	}
}
