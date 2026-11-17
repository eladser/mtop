package gpu

// Debounce turns raw per-poll throttle flags into the flags worth showing:
// a flag has to be set with the card busy (Util >= 30) for 3 polls in a
// row to show, and clear for 3 polls in a row to hide. hw_slowdown flickers
// on P-state changes, so showing it raw would be noise. The zero value is
// ready; call Update once per poll.
type Debounce struct {
	gpus []debounced
}

const (
	debounceN    = 3
	throttleUtil = 30
	nbits        = 3
)

type debounced struct {
	hit, clean [nbits]uint8
	shown      uint8
}

// Update takes one poll's stats and returns the shown flags per GPU, same
// order. State resets when the GPU count changes.
func (d *Debounce) Update(gs []Stats) []uint8 {
	if len(d.gpus) != len(gs) {
		d.gpus = make([]debounced, len(gs))
	}
	out := make([]uint8, len(gs))
	for i, g := range gs {
		s := &d.gpus[i]
		for b := 0; b < nbits; b++ {
			bit := uint8(1) << b
			if g.Throttle&bit != 0 && g.Util >= throttleUtil {
				s.clean[b] = 0
				if s.hit[b] < debounceN {
					s.hit[b]++
				}
				if s.hit[b] >= debounceN {
					s.shown |= bit
				}
			} else {
				s.hit[b] = 0
				if s.clean[b] < debounceN {
					s.clean[b]++
				}
				if s.clean[b] >= debounceN {
					s.shown &^= bit
				}
			}
		}
		out[i] = s.shown
	}
	return out
}
