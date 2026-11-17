package proxy

import (
	"sort"
	"sync"
	"time"
)

// streamGuard is how long a streaming flight can go without a start or
// a tick before Flights stops listing it, so a body nobody closed
// doesn't sit in the pane forever.
const streamGuard = 10 * time.Minute

// Live is one request in flight, registered before the upstream round
// trip. The tap feeds it chunks; the ui reads it through Store.Flights.
type Live struct {
	mu       sync.Mutex
	store    *Store
	id       uint64
	path     string
	model    string
	stream   bool
	started  time.Time
	firstTok time.Time
	last     time.Time // latest start or tick, for the leak guard
	tokens   int
}

type Flight struct {
	ID                uint64
	Path, Model       string
	Stream            bool
	Started, FirstTok time.Time
	Tokens            int
}

// Begin registers a request that's about to go upstream.
func (s *Store) Begin(path, model string, stream bool, started time.Time) *Live {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	l := &Live{store: s, id: s.nextID, path: path, model: model, stream: stream, started: started, last: started}
	if s.flights == nil {
		s.flights = map[uint64]*Live{}
	}
	s.flights[l.id] = l
	return l
}

// Tick counts n content chunks that arrived at the given time. It does
// nothing for a non-streaming request: that response is one object with
// the whole text, which would read as a single token.
func (l *Live) Tick(n int, at time.Time) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.stream || n <= 0 {
		return
	}
	if l.firstTok.IsZero() {
		l.firstTok = at
	}
	l.tokens += n
	l.last = at
}

// SetModel replaces the request's model name with the one the server
// put in its first response chunk (llama.cpp sends its alias).
func (l *Live) SetModel(m string) {
	if l == nil || m == "" {
		return
	}
	l.mu.Lock()
	l.model = m
	l.mu.Unlock()
}

// Complete drops the flight and records r in one step, so the pane never
// shows both or neither. Works even if Flights had stopped listing it.
func (s *Store) Complete(l *Live, r Request) {
	s.mu.Lock()
	if l != nil {
		delete(s.flights, l.id)
	}
	fn := s.addLocked(r)
	s.mu.Unlock()
	if fn != nil {
		fn(r)
	}
}

// End drops the flight without recording anything. Safe to call twice.
func (l *Live) End() {
	if l == nil {
		return
	}
	l.store.mu.Lock()
	delete(l.store.flights, l.id)
	l.store.mu.Unlock()
}

// Flights returns copies of what's in flight, oldest first.
func (s *Store) Flights() []Flight {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	s.mu.Lock()
	ls := make([]*Live, 0, len(s.flights))
	for _, l := range s.flights {
		ls = append(ls, l)
	}
	s.mu.Unlock()

	out := make([]Flight, 0, len(ls))
	for _, l := range ls {
		l.mu.Lock()
		f := Flight{ID: l.id, Path: l.path, Model: l.model, Stream: l.stream,
			Started: l.started, FirstTok: l.firstTok, Tokens: l.tokens}
		stale := l.stream && now.Sub(l.last) > streamGuard
		l.mu.Unlock()
		if !stale {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
