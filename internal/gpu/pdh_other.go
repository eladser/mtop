//go:build !windows

package gpu

import "errors"

type pdhReader struct{}

func newPDHReader() *pdhReader { return nil }

func (p *pdhReader) read() ([]Stats, error) { return nil, errors.New("pdh: windows only") }
