// Package verdict decides why a loaded model is slow, from what the
// servers report and what the proxy saw. Pure logic: the caller feeds a
// snapshot every poll and keeps the clock.
package verdict

import (
	"fmt"
	"time"
)

type Level uint8

const (
	Red Level = iota + 1
	Amber
)

type Kind uint8

const (
	CtxFull Kind = iota + 1
	CtxCut
	CPU
	Queue
	CtxNear
)

type Verdict struct {
	Kind  Kind
	Level Level
	Text  string
}

const (
	failWindow = 5 * time.Minute
	nearWindow = 10 * time.Minute
	nearOn     = 80
	nearOff    = 70
)

// Req is a finished request matched to a model.
type Req struct {
	When     time.Time
	Ctx      int // the model's context length when it ran, 0 if unknown
	PromptTk int
	Overflow bool // the server rejected it as over context
}

func (r Req) pct() int {
	if r.Ctx <= 0 {
		return -1
	}
	return r.PromptTk * 100 / r.Ctx
}

// Model is one loaded model's snapshot for a poll. Key must be unique
// across hosts, e.g. From + "|" + Name.
type Model struct {
	Key     string
	CPU     int   // percent of the model in system RAM
	VRAM    int64 // 0 when the server doesn't report it
	Queued  int   // server-reported waiting requests
	Slots   int   // 0 when unknown
	Flights int   // proxy requests in flight matched to this model
	Reqs    []Req // finished requests matched to this model, any order
}

type state struct {
	qOn, qOff int
	queued    bool
	qText     string
	nearShown bool
}

type Tracker struct {
	start time.Time
	st    map[string]*state
	gone  map[string]time.Time // when a key last disappeared, requests before it are stale
}

// New starts a tracker. Requests from before start (history from an
// earlier session) never count.
func New(start time.Time) *Tracker {
	return &Tracker{start: start, st: map[string]*state{}, gone: map[string]time.Time{}}
}

// Update takes one poll's snapshot and returns the verdicts by Key.
// Models with nothing to say are absent.
func (t *Tracker) Update(now time.Time, ms []Model) map[string]Verdict {
	out := map[string]Verdict{}
	seen := map[string]bool{}
	for _, m := range ms {
		seen[m.Key] = true
		s := t.st[m.Key]
		if s == nil {
			s = &state{}
			t.st[m.Key] = s
		}
		cut := t.start
		if g, ok := t.gone[m.Key]; ok && g.After(cut) {
			cut = g
		}
		if v, ok := t.eval(now, cut, s, m); ok {
			out[m.Key] = v
		}
	}
	for k := range t.st {
		if !seen[k] {
			delete(t.st, k)
			t.gone[k] = now
		}
	}
	return out
}

func (t *Tracker) eval(now, cut time.Time, s *state, m Model) (Verdict, bool) {
	var full, trunc, near *Req
	var newest *Req
	for i := range m.Reqs {
		r := &m.Reqs[i]
		if r.When.Before(cut) || r.Ctx <= 0 {
			continue
		}
		age := now.Sub(r.When)
		if age < failWindow {
			if r.Overflow {
				full = r
			} else if r.PromptTk >= r.Ctx {
				trunc = r
			}
		}
		if age < nearWindow && (newest == nil || r.When.After(newest.When)) {
			newest = r
		}
	}
	if newest != nil {
		p := newest.pct()
		switch {
		case p >= nearOn && p < 100, s.nearShown && p >= nearOff && p < 100:
			near = newest
			s.nearShown = true
		default:
			s.nearShown = false
		}
	} else {
		s.nearShown = false
	}

	// queue state moves every poll, whatever rule wins below
	text := ""
	switch {
	case m.Queued >= 1:
		text = fmt.Sprintf("%d %s queued", m.Queued, plural(m.Queued, "request"))
	case m.Slots > 0 && m.Flights > m.Slots:
		text = fmt.Sprintf("%d requests, %d %s", m.Flights, m.Slots, plural(m.Slots, "slot"))
	}
	if text != "" {
		s.qOn++
		s.qOff = 0
		s.qText = text
		if s.qOn >= 2 {
			s.queued = true
		}
	} else {
		s.qOff++
		s.qOn = 0
		if s.qOff >= 2 {
			s.queued = false
		}
	}

	switch {
	case full != nil:
		return Verdict{CtxFull, Red, "rejected: context full"}, true
	case trunc != nil:
		return Verdict{CtxCut, Red, "prompt cut to fit context"}, true
	case m.VRAM > 0 && m.CPU >= 10 && m.CPU <= 99:
		return Verdict{CPU, Red, fmt.Sprintf("%d%% of model on CPU", m.CPU)}, true
	case s.queued:
		// text is empty during the 2-poll clear window, so keep the last one
		return Verdict{Queue, Amber, s.qText}, true
	case near != nil:
		return Verdict{CtxNear, Amber, fmt.Sprintf("context %d%% full", near.pct())}, true
	}
	return Verdict{}, false
}

func plural(n int, w string) string {
	if n == 1 {
		return w
	}
	return w + "s"
}
