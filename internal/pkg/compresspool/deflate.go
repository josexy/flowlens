package compresspool

import (
	"compress/flate"
	"io"
	"sync"
)

var (
	flateReaderPool sync.Pool
)

type flateReader interface {
	io.ReadCloser
	flate.Resetter
}

func AcquireFlateReader(r io.Reader) (flateReader, error) {
	if value := flateReaderPool.Get(); value != nil {
		fr := value.(flateReader)
		if err := fr.Reset(r, nil); err != nil {
			return nil, err
		}
		return fr, nil
	}
	return flate.NewReader(r).(flateReader), nil
}

func ReleaseFlateReader(fr flateReader) {
	if fr == nil {
		return
	}
	if err := fr.Reset(newEmptyReader(), nil); err != nil {
		return
	}
	flateReaderPool.Put(fr)
}
