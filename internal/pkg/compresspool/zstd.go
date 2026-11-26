package compresspool

import (
	"errors"
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

// zstdDecoderMaxWindow caps the window a frame may declare. A decoder
// allocates the declared window when a frame starts and keeps it while
// pooled, so with the library's 512 MiB limit a response of a few bytes held
// over 512 MiB. RFC 9659 limits zstd content coding to 8 MiB, and HBIN
// frames never used more than the 8 MiB default of older builds. Frames over
// the cap fail to decode.
const zstdDecoderMaxWindow = 8 << 20

// zstdReadCloser preserves the last decoded block on a checksum error.
// The embedded decoder's WriteTo already writes that block before the error.
type zstdReadCloser struct {
	*zstd.Decoder
}

func (r zstdReadCloser) Read(p []byte) (int, error) {
	n, err := r.Decoder.Read(p)
	if n == 0 && errors.Is(err, zstd.ErrCRCMismatch) {
		// In v1.20.5, synchronous Read reports the checksum error before
		// returning the last block it decoded. Read again to drain that
		// buffered output; the sticky error prevents further source reads
		// and is returned once the buffered bytes have been consumed.
		return r.Decoder.Read(p)
	}
	return n, err
}

func (r zstdReadCloser) Close() error {
	r.Decoder.Close()
	return nil
}

func AcquireZstdDecoder(r io.Reader) (*zstd.Decoder, error) {
	if value := zstdDecoderPool.Get(); value != nil {
		dec := value.(*zstd.Decoder)
		if err := dec.Reset(r); err != nil {
			return nil, err
		}
		return dec, nil
	}
	return zstd.NewReader(r,
		// Decode only as Read asks. The library's read-ahead goroutines made a
		// Read after a decoding error, and Close, wait on a source Read that an
		// open, stalled response never finishes, and kept the last source
		// reachable from the pooled decoder.
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderMaxWindow(zstdDecoderMaxWindow),
	)
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
