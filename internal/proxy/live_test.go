package proxy

import (
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func TestFlightLifecycle(t *testing.T) {
	s := testStore()
	a := s.Begin("/api/chat", "m1", true, t0)
	b := s.Begin("/v1/chat/completions", "m2", false, t0.Add(time.Second))
	fl := s.Flights()
	if len(fl) != 2 || fl[0].Model != "m1" || fl[1].Model != "m2" || fl[0].ID >= fl[1].ID {
		t.Fatalf("want two flights oldest first, got %+v", fl)
	}
	a.Tick(1, t0.Add(400*time.Millisecond))
	a.Tick(2, t0.Add(500*time.Millisecond))
	fl = s.Flights()
	if fl[0].Tokens != 3 || !fl[0].FirstTok.Equal(t0.Add(400*time.Millisecond)) {
		t.Fatalf("ticks: %+v", fl[0])
	}
	a.SetModel("alias")
	a.SetModel("")
	if s.Flights()[0].Model != "alias" {
		t.Fatal("SetModel should replace, empty should not")
	}
	b.End()
	b.End()
	if len(s.Flights()) != 1 {
		t.Fatalf("End should be idempotent: %+v", s.Flights())
	}
	s.Complete(a, Request{Model: "alias", OutTk: 3})
	if len(s.Flights()) != 0 || len(s.Recent(5)) != 1 {
		t.Fatal("Complete should remove the flight and record the request")
	}
}

func TestTickNoopWhenNotStreaming(t *testing.T) {
	s := testStore()
	l := s.Begin("/api/generate", "m", false, t0)
	l.Tick(1, t0.Add(time.Second))
	f := s.Flights()[0]
	if f.Tokens != 0 || !f.FirstTok.IsZero() {
		t.Fatalf("non-streaming flight ticked: %+v", f)
	}
}

func TestFlightsLeakGuard(t *testing.T) {
	s := testStore()
	s.now = func() time.Time { return t0.Add(11 * time.Minute) }
	stream := s.Begin("/api/chat", "s", true, t0)
	s.Begin("/api/chat", "long", false, t0)
	fl := s.Flights()
	if len(fl) != 1 || fl[0].Model != "long" {
		t.Fatalf("stale stream should hide, non-streaming should stay: %+v", fl)
	}
	stream.Tick(1, t0.Add(5*time.Minute))
	if len(s.Flights()) != 2 {
		t.Fatal("a tick should bring the stream back")
	}
	s.now = func() time.Time { return t0.Add(20 * time.Minute) }
	s.Complete(stream, Request{Model: "s"})
	if len(s.Recent(1)) != 1 {
		t.Fatal("Complete must record a hidden flight")
	}
}

func TestCompleteFiresOnAdd(t *testing.T) {
	s := testStore()
	var got []Request
	s.OnAdd(func(r Request) { got = append(got, r) })
	s.Complete(s.Begin("/x", "m", true, t0), Request{Model: "m"})
	s.Complete(nil, Request{Model: "n"})
	if len(got) != 2 {
		t.Fatalf("onAdd calls: %d", len(got))
	}
}

func TestLiveConcurrent(t *testing.T) {
	s := testStore()
	l := s.Begin("/x", "m", true, t0)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Tick(1, t0)
				s.Flights()
			}
		}()
	}
	wg.Wait()
	if s.Flights()[0].Tokens != 800 {
		t.Fatalf("tokens %d", s.Flights()[0].Tokens)
	}
}

func testStore() *Store {
	s := NewStore(10)
	s.now = func() time.Time { return t0.Add(time.Minute) }
	return s
}
