package gpu

import (
	"os/exec"
	"strings"
	"sync"
)

const nvBase = "name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw"

// Newer drivers call the throttle fields clocks_event_reasons, older ones
// clocks_throttle_reasons (the alias). An unknown field fails the whole
// query with exit 2, so the last rung drops them and the pane still works.
var nvQueries = []string{
	nvBase + ",clocks_event_reasons.hw_thermal_slowdown,clocks_event_reasons.hw_power_brake_slowdown,clocks_event_reasons.hw_slowdown",
	nvBase + ",clocks_throttle_reasons.hw_thermal_slowdown,clocks_throttle_reasons.hw_power_brake_slowdown,clocks_throttle_reasons.hw_slowdown",
	nvBase,
}

// runSMI is swapped out in tests.
var runSMI = func(path string, args ...string) ([]byte, error) {
	return exec.Command(path, args...).Output()
}

// smiLadder remembers which query the driver accepted so later polls
// don't retry the ones that failed.
type smiLadder struct {
	mu      sync.Mutex
	decided bool
	step    int
}

func readNvidia(l *smiLadder, path string) ([]Stats, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	first, last := 0, len(nvQueries)-1
	if l.decided {
		first, last = l.step, l.step
	}
	var err error
	for i := first; i <= last; i++ {
		var out []byte
		out, err = runSMI(path, "--query-gpu="+nvQueries[i], "--format=csv,noheader,nounits")
		if err == nil {
			l.decided, l.step = true, i
			return parse(string(out)), nil
		}
	}
	return nil, err
}

// parseReason reads one clocks_event_reasons field: "Active" is set,
// "Not Active", "[N/A]" and "[Not Supported]" are clear.
func parseReason(s string) bool {
	return strings.TrimSpace(s) == "Active"
}
