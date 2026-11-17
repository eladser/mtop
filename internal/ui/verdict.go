package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/sources"
	"github.com/eladser/mtop/internal/verdict"
)

var amberSt = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB454"))

// verdictStyle is the color a verdict line gets: red for failures and
// structural slowdowns, amber for the transient ones.
func verdictStyle(l verdict.Level) lipgloss.Style {
	if l == verdict.Amber {
		return amberSt
	}
	return warnSt
}

func rowKey(r sources.Row) string { return r.From + "|" + r.Name }

// verdictFor is the why-slow verdict of row i, if it has one.
func (a *App) verdictFor(i int) (verdict.Verdict, bool) {
	if i < 0 || i >= len(a.rows) {
		return verdict.Verdict{}, false
	}
	v, ok := a.verdicts[rowKey(a.rows[i])]
	return v, ok
}

func (a *App) hasVerdict(i int) bool {
	_, ok := a.verdictFor(i)
	return ok
}

// verdictLine is the selected row's why-slow line under the models table,
// "" when there is none. lineW is the models pane's line width; the text
// gets lineW-4 after the "  ↳ " lead.
func (a *App) verdictLine(lineW int) string {
	v, ok := a.verdictFor(a.sel)
	if !ok {
		return ""
	}
	return "  ↳ " + trunc(v.Text, lineW-4)
}

// matchRow finds the row a request for model went to, -1 when unsure.
// Only rows on the proxy's upstream are candidates, then by name, alias,
// any alias, name+":latest", and last the only candidate. No guess from
// the request path: the default target is Ollama, whose /v1 traffic would
// otherwise land on an unrelated row.
func (a *App) matchRow(model string) int {
	if a.target == "" || model == "" {
		return -1
	}
	var cand []int
	for i, r := range a.rows {
		if r.Base == a.target {
			cand = append(cand, i)
		}
	}
	pass := func(eq func(r sources.Row) bool) int {
		for _, i := range cand {
			if eq(a.rows[i]) {
				return i
			}
		}
		return -1
	}
	is := func(s string) bool { return strings.EqualFold(s, model) }
	if i := pass(func(r sources.Row) bool { return is(r.Name) }); i >= 0 {
		return i
	}
	if i := pass(func(r sources.Row) bool { return r.Alias != "" && is(r.Alias) }); i >= 0 {
		return i
	}
	if i := pass(func(r sources.Row) bool {
		for _, al := range r.Aliases {
			if is(al) {
				return true
			}
		}
		return false
	}); i >= 0 {
		return i
	}
	if i := pass(func(r sources.Row) bool { return strings.EqualFold(r.Name, model+":latest") }); i >= 0 {
		return i
	}
	if len(cand) == 1 {
		return cand[0]
	}
	return -1
}

// judge runs once per poll: it feeds the verdict tracker and the GPU
// throttle debounce. Not on the live tick, so lines don't flicker.
func (a *App) judge() {
	if a.vtrack == nil {
		start := a.start
		if start.IsZero() {
			start = a.clock()
		}
		a.vtrack = verdict.New(start)
	}
	ms := make([]verdict.Model, len(a.rows))
	for i, r := range a.rows {
		ms[i] = verdict.Model{Key: rowKey(r), CPU: r.CPU, VRAM: r.VRAM, Queued: r.Queued, Slots: r.Slots}
	}
	for _, r := range a.store.Recent(256) {
		if i := a.matchRow(r.Model); i >= 0 {
			ms[i].Reqs = append(ms[i].Reqs, verdict.Req{When: r.When, Ctx: r.Ctx, PromptTk: r.PromptTk, Overflow: r.Overflow})
		}
	}
	for _, f := range a.store.Flights() {
		if i := a.matchRow(f.Model); i >= 0 {
			ms[i].Flights++
		}
	}
	a.verdicts = a.vtrack.Update(a.clock(), ms)
	a.throttled = a.gdeb.Update(a.gpus)
}

// throttleText is the GPU pane line for gpu i, the worst shown flag.
func (a *App) throttleText(i int) string {
	if i >= len(a.throttled) {
		return ""
	}
	switch t := a.throttled[i]; {
	case t&gpu.ThrottleThermal != 0:
		return "hw thermal slowdown"
	case t&gpu.ThrottlePower != 0:
		return "hw power brake"
	case t&gpu.ThrottleHW != 0:
		return "hw slowdown"
	}
	return ""
}

// throttleLines is how many hardware-throttle lines the GPU pane adds.
func (a *App) throttleLines() int {
	if a.gpu == nil || !a.gpu.Available() || a.gpuErr != nil {
		return 0
	}
	n := 0
	for i := range a.gpus {
		if a.throttleText(i) != "" {
			n++
		}
	}
	return n
}
