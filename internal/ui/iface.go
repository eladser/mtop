package ui

import (
	"github.com/eladser/mtop/internal/gpu"
	"github.com/eladser/mtop/internal/sources"
)

// Scanner and GPU are what App needs from sources.Scanner and gpu.Reader,
// so a simulated pair can stand in for them.
type Scanner interface {
	Scan() ([]sources.Row, []string, error)
	OnDisk() int
}

type GPU interface {
	Available() bool
	Read() ([]gpu.Stats, error)
}

var (
	_ Scanner = (*sources.Scanner)(nil)
	_ GPU     = (*gpu.Reader)(nil)
)
