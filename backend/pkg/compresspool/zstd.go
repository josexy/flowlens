package compresspool

import (
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var (
	zstdDecoderPool sync.Pool
	zstdEncoderPool sync.Pool
)

func AcquireZstdDecoder(r io.Reader) (*zstd.Decoder, error) {
	if value := zstdDecoderPool.Get(); value != nil {
		dec := value.(*zstd.Decoder)
		if err := dec.Reset(r); err != nil {
			return nil, err
		}
		return dec, nil
	}
	return zstd.NewReader(r)
}

func ReleaseZstdDecoder(dec *zstd.Decoder) {
	if dec == nil {
		return
	}
	if err := dec.Reset(nil); err != nil {
		return
	}
	zstdDecoderPool.Put(dec)
}

func AcquireZstdEncoder(w io.Writer) (*zstd.Encoder, error) {
	if value := zstdEncoderPool.Get(); value != nil {
		enc := value.(*zstd.Encoder)
		enc.Reset(w)
		return enc, nil
	}
	return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
}

func ReleaseZstdEncoder(enc *zstd.Encoder) {
	if enc == nil {
		return
	}
	enc.Reset(nil)
	zstdEncoderPool.Put(enc)
}
