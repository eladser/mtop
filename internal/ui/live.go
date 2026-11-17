package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/eladser/mtop/internal/proxy"
)

const (
	liveEvery  = 250 * time.Millisecond
	liveWindow = 2 * time.Second
)

type liveTick struct{}

func liveTickCmd() tea.Cmd {
	return tea.Tick(liveEvery, func(time.Time) tea.Msg { return liveTick{} })
}

// liveSample is a flight's chunk count at one tick, kept for the
// trailing rate.
type liveSample struct {
	at time.Time
	n  int
}

// sampleLive records every flight's count and forgets flights that are gone.
func (a *App) sampleLive() {
	now := a.clock()
	if a.live == nil {
		a.live = map[uint64][]liveSample{}
	}
	cur := map[uint64]bool{}
	for _, f := range a.store.Flights() {
		cur[f.ID] = true
		s := append(a.live[f.ID], liveSample{now, f.Tokens})
		i := 0
		for i < len(s)-1 && now.Sub(s[i].at) > liveWindow {
			i++
		}
		a.live[f.ID] = s[i:]
	}
	for id := range a.live {
		if !cur[id] {
			delete(a.live, id)
		}
	}
}

// liveRate is chunks per second over the samples in the trailing window.
func liveRate(s []liveSample, now time.Time) (float64, bool) {
	var in []liveSample
	for _, x := range s {
		if now.Sub(x.at) <= liveWindow {
			in = append(in, x)
		}
	}
	if len(in) < 2 {
		return 0, false
	}
	first, last := in[0], in[len(in)-1]
	dt := last.at.Sub(first.at).Seconds()
	if dt <= 0 {
		return 0, false
	}
	return float64(last.n-first.n) / dt, true
}

// flightRate is the live rate of one flight, shown as "…" until it has
// 3 chunks and has been decoding for 300ms.
func (a *App) flightRate(f proxy.Flight, now time.Time) (float64, bool) {
	if !f.Stream || f.FirstTok.IsZero() || f.Tokens < 3 || now.Sub(f.FirstTok) < 300*time.Millisecond {
		return 0, false
	}
	return liveRate(a.live[f.ID], now)
}

// liveTotal sums the rates of the flights that have one.
func (a *App) liveTotal() (float64, bool) {
	now := a.clock()
	var sum float64
	var ok bool
	for _, f := range a.store.Flights() {
		if r, k := a.flightRate(f, now); k {
			sum += r
			ok = true
		}
	}
	return sum, ok
}

// liveSplit divides r request rows between f flights and finished
// requests: all flights when they leave at least 2 finished rows,
// otherwise r-3 of them plus a "+more" row and 2 finished.
func liveSplit(f, r int) (live, more, fin int) {
	if f <= r-2 {
		return f, 0, r - f
	}
	return r - 3, f - (r - 3), 2
}

// liveTail is the dim text after a live row's TOTAL: the longest variant
// that fits in avail columns, or "".
func liveTail(f proxy.Flight, now time.Time, avail int) string {
	var opts []string
	switch {
	case !f.Stream:
		opts = []string{"not streaming"}
	case f.FirstTok.IsZero():
		opts = []string{"prefill"}
	default:
		ttft := f.FirstTok.Sub(f.Started).Seconds()
		dec := max(now.Sub(f.FirstTok), 0).Seconds()
		opts = []string{fmt.Sprintf("ttft %.2fs · dec %.1fs", ttft, dec), fmt.Sprintf("ttft %.2fs", ttft)}
	}
	for _, o := range opts {
		if len([]rune(o)) <= avail {
			return o
		}
	}
	return ""
}

// liveElapsed is the TOTAL cell of a live row: 3.9s, 1m05s past 100s.
func liveElapsed(d time.Duration) string {
	d = max(d, 0).Round(100 * time.Millisecond)
	if d < 100*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

// liveClock is the TIME cell of a live row, right-aligned in 8 columns:
// 3.2s under a minute, 2m05s after.
func liveClock(d time.Duration) string {
	d = max(d, 0).Round(100 * time.Millisecond)
	if d < 60*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}

// liveRows renders the in-flight requests for the top of the requests
// pane, given the pane's r data rows, and says how many of those rows are
// left for finished requests.
func (a *App) liveRows(r int) (rows []string, fin int) {
	fl := a.store.Flights()
	if len(fl) == 0 {
		return nil, r
	}
	n, more, fin := liveSplit(len(fl), r)
	now := a.clock()
	avail := a.w - 6 - 70 - 2
	for _, f := range fl[:n] {
		rows = append(rows, a.liveRow(f, now, avail))
	}
	if more > 0 {
		rows = append(rows, dimSt.Render(fmt.Sprintf("+%d more live", more)))
	}
	return rows, fin
}

func (a *App) liveRow(f proxy.Flight, now time.Time, avail int) string {
	model := trunc(clean(f.Model), 26)
	if model == "" {
		model = "?"
	}
	tok, out := "-", "-"
	if rate, ok := a.flightRate(f, now); ok {
		tok = fmt.Sprintf("%.1f", rate)
	} else if !f.FirstTok.IsZero() {
		tok = "…"
	}
	if f.Tokens > 0 {
		out = fmt.Sprintf("~%d", f.Tokens)
	}
	var b strings.Builder
	b.WriteString(selSt.Render("▶"))
	fmt.Fprintf(&b, "%8s %-26s %10s %6s %8s  %s", liveClock(now.Sub(f.Started)), model, tok, out, "-", liveElapsed(now.Sub(f.Started)))
	if t := liveTail(f, now, avail); t != "" {
		b.WriteString("  " + dimSt.Render(t))
	}
	return b.String()
}
