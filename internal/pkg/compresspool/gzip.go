package compresspool

import (
	"io"
	"sync"

	"github.com/klauspost/compress/gzip"
)

var (
	gzipReaderPool sync.Pool

	emptyGzipStream = mustEncodeEmptyGzipStream()
)

func AcquireGzipReader(r io.Reader) (*gzip.Reader, error) {
	if value := gzipReaderPool.Get(); value != nil {
		gr := value.(*gzip.Reader)
		if err := gr.Reset(r); err != nil {
			return nil, err
		}
		return gr, nil
	}
	return gzip.NewReader(r)
}

func ReleaseGzipReader(gr *gzip.Reader) {
	if gr == nil {
		return
	}
	gr.Reset(newEmptyGzipReader())
	gzipReaderPool.Put(gr)
}
