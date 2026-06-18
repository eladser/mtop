package ui

import "strings"

var sparks = []rune("▁▂▃▄▅▆▇█")

// sparkPct renders 0..100 values on a fixed scale, so 10% reads low and
// 90% reads high instead of both filling the line.
func sparkPct(vals []float64) string {
	var b strings.Builder
	for _, v := range vals {
		i := int(v / 100 * float64(len(sparks)-1))
		if i < 0 {
			i = 0
		}
		if i >= len(sparks) {
			i = len(sparks) - 1
		}
		b.WriteRune(sparks[i])
	}
	return b.String()
}

func sparkline(vals []float64, width int) string {
	if len(vals) == 0 || width <= 0 {
		return ""
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	max := 0.0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return strings.Repeat(string(sparks[0]), len(vals))
	}
	var b strings.Builder
	for _, v := range vals {
		b.WriteRune(sparks[int(v/max*float64(len(sparks)-1))])
	}
	return b.String()
}
