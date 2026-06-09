package ui

import "strings"

var sparks = []rune("▁▂▃▄▅▆▇█")

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
