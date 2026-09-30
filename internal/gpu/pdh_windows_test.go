//go:build windows

package gpu

import "testing"

// Real PDH counters, real registry. Skips instead of failing on machines
// without a display adapter exposing the GPU Engine/Adapter Memory
// counter sets (headless CI, RDP-only session host).
func TestPDHSmoke(t *testing.T) {
	p := newPDHReader()
	if p == nil {
		t.Skip("pdh: reader unavailable on this machine")
	}
	stats, err := p.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(stats) == 0 {
		t.Skip("pdh: no adapters reported by PDH")
	}
	t.Logf("pdh saw %d adapter(s): %+v", len(stats), stats)
}
