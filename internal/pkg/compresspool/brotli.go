package compresspool

import (
	"io"
	"sync"

	"github.com/andybalholm/brotli"
)

var (
	brotliReaderPool sync.Pool
)

func AcquireBrotliReader(r io.Reader) (*brotli.Reader, error) {
	if value := brotliReaderPool.Get(); value != nil {
		br := value.(*brotli.Reader)
		if err := br.Reset(r); err != nil {
			return nil, err
		}
		return br, nil
	}
	return brotli.NewReader(r), nil
}

func ReleaseBrotliReader(br *brotli.Reader) {
	if br == nil {
		return
	}
	if err := br.Reset(newEmptyReader()); err != nil {
		return
	}
	brotliReaderPool.Put(br)
}
