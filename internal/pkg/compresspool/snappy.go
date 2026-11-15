package compresspool

import (
	"io"
	"sync"

	"github.com/klauspost/compress/snappy"
)

var (
	snappyReaderPool sync.Pool
)

func AcquireSnappyReader(r io.Reader) *snappy.Reader {
	if value := snappyReaderPool.Get(); value != nil {
		sr := value.(*snappy.Reader)
		sr.Reset(r)
		return sr
	}
	return snappy.NewReader(r)
}

func ReleaseSnappyReader(sr *snappy.Reader) {
	if sr == nil {
		return
	}
	sr.Reset(newEmptyReader())
	snappyReaderPool.Put(sr)
}
