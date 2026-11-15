package compresspool

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zlib"
)

type pooledReadCloser struct {
	reader  io.ReadCloser
	release func()
	once    sync.Once
}

func (r *pooledReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *pooledReadCloser) Close() error {
	r.once.Do(func() {
		if r.release != nil {
			r.release()
		}
	})
	return nil
}

func (r *pooledReadCloser) WriteTo(w io.Writer) (int64, error) {
	if wt, ok := r.reader.(io.WriterTo); ok {
		return wt.WriteTo(w)
	}
	return io.Copy(w, r.reader)
}

func NewPooledReadCloser(reader io.ReadCloser, release func()) io.ReadCloser {
	return &pooledReadCloser{
		reader:  reader,
		release: release,
	}
}

func NewPooledGzipReadCloser(r io.Reader) (io.ReadCloser, error) {
	gr, err := AcquireGzipReader(r)
	if err != nil {
		return nil, err
	}
	return NewPooledReadCloser(gr, func() {
		ReleaseGzipReader(gr)
	}), nil
}

func NewPooledBrotliReadCloser(r io.Reader) (io.ReadCloser, error) {
	br, err := AcquireBrotliReader(r)
	if err != nil {
		return nil, err
	}
	return NewPooledReadCloser(io.NopCloser(br), func() {
		ReleaseBrotliReader(br)
	}), nil
}

func NewPooledSnappyReadCloser(r io.Reader) io.ReadCloser {
	sr := AcquireSnappyReader(r)
	return NewPooledReadCloser(io.NopCloser(sr), func() {
		ReleaseSnappyReader(sr)
	})
}

func NewPooledDeflateReadCloser(r io.Reader) (io.ReadCloser, error) {
	buffered, ok := r.(*bufio.Reader)
	if !ok {
		buffered = bufio.NewReader(r)
	}
	if looksLikeZlibStream(buffered) {
		zr, err := AcquireZlibReader(buffered)
		if err != nil {
			return nil, err
		}
		return NewPooledReadCloser(zr, func() {
			ReleaseZlibReader(zr)
		}), nil
	}

	fr, err := AcquireFlateReader(buffered)
	if err != nil {
		return nil, err
	}
	return NewPooledReadCloser(fr, func() {
		ReleaseFlateReader(fr)
	}), nil
}

func looksLikeZlibStream(r *bufio.Reader) bool {
	header, err := r.Peek(2)
	if err != nil || len(header) < 2 {
		return false
	}
	cmf := header[0]
	flg := header[1]
	if cmf&0x0f != 8 {
		return false
	}
	if cmf>>4 > 7 {
		return false
	}
	return ((uint16(cmf)<<8)|uint16(flg))%31 == 0
}

func newEmptyReader() io.Reader {
	return bytes.NewReader(nil)
}

func newEmptyGzipReader() io.Reader {
	return bytes.NewReader(emptyGzipStream)
}

func newEmptyZlibReader() io.Reader {
	return bytes.NewReader(emptyZlibStream)
}

func mustEncodeEmptyGzipStream() []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return append([]byte(nil), buf.Bytes()...)
}

func mustEncodeEmptyZlibStream() []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return append([]byte(nil), buf.Bytes()...)
}

func NewPooledZstdReadCloser(r io.Reader) (io.ReadCloser, error) {
	dec, err := AcquireZstdDecoder(r)
	if err != nil {
		return nil, err
	}
	return NewPooledReadCloser(dec.IOReadCloser(), func() {
		ReleaseZstdDecoder(dec)
	}), nil
}
