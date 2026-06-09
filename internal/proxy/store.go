package proxy

import (
	"sync"
	"time"
)

type Request struct {
	When     time.Time
	Path     string
	Model    string
	PromptTk int
	OutTk    int
	TokSec   float64
	Total    time.Duration
}

// Store keeps the last N proxied requests, newest first.
type Store struct {
	mu   sync.Mutex
	reqs []Request
	max  int
	err  error
}

func NewStore(max int) *Store {
	return &Store{max: max}
}

func (s *Store) Add(r Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append([]Request{r}, s.reqs...)
	if len(s.reqs) > s.max {
		s.reqs = s.reqs[:s.max]
	}
}

func (s *Store) Recent(n int) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.reqs) {
		n = len(s.reqs)
	}
	out := make([]Request, n)
	copy(out, s.reqs[:n])
	return out
}

// TokRates returns tok/s of the last n requests, oldest first.
func (s *Store) TokRates(n int) []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > len(s.reqs) {
		n = len(s.reqs)
	}
	out := make([]float64, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, s.reqs[i].TokSec)
	}
	return out
}

// LastSeen returns when a model last handled a request through the
// proxy, or zero if it never did.
func (s *Store) LastSeen(model string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.reqs {
		if r.Model == model {
			return r.When
		}
	}
	return time.Time{}
}

func (s *Store) SetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
