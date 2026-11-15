package compresspool

import (
	"io"
	"sync"

	"github.com/klauspost/compress/zlib"
)

var (
	zlibReaderPool sync.Pool

	emptyZlibStream = mustEncodeEmptyZlibStream()
)

type zlibReader interface {
	io.ReadCloser
	zlib.Resetter
}

func AcquireZlibReader(r io.Reader) (zlibReader, error) {
	if value := zlibReaderPool.Get(); value != nil {
		zr := value.(zlibReader)
		if err := zr.Reset(r, nil); err != nil {
			return nil, err
		}
		return zr, nil
	}
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, err
	}
	return zr.(zlibReader), nil
}

func ReleaseZlibReader(zr zlibReader) {
	if zr == nil {
		return
	}
	zr.Reset(newEmptyZlibReader(), nil)
	zlibReaderPool.Put(zr)
}
