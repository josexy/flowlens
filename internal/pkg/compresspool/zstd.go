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

// zstdEncoderWindow bounds what a pooled encoder keeps once it has written a
// body over one 128 KiB block: a history buffer twice the window. With the
// library's 8 MiB default, each such encoder held about 19 MiB while pooled;
// with 1 MiB it holds about 5 MiB, at the same ratio for ordinary bodies.
// Only repeats more than 1 MiB apart compress less. Decoders read frames of
// either window.
const zstdEncoderWindow = 1 << 20

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
	return zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithWindowSize(zstdEncoderWindow))
}

func ReleaseZstdEncoder(enc *zstd.Encoder) {
	if enc == nil {
		return
	}
	enc.Reset(nil)
	zstdEncoderPool.Put(enc)
}
