package historyservice

import (
	"context"
	"io"
)

type historyContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r historyContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
