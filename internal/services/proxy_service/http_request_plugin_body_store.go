package proxyservice

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

func httpRequestPluginResponseBodyKindFromFile(
	ctx context.Context,
	contentType string,
	file *HTTPRequestPluginBodyFile,
) (string, error) {
	size, err := httpRequestPluginBodyFileSize(ctx, file)
	if err != nil {
		return "", err
	}
	if size == 0 {
		return "none", nil
	}

	mediaType, _, mediaTypeErr := mime.ParseMediaType(contentType)
	if mediaTypeErr == nil && (strings.EqualFold(mediaType, "application/json") || strings.HasSuffix(strings.ToLower(mediaType), "+json")) {
		valid, validationErr := validateHTTPRequestPluginResponseFileWith(ctx, file, validateJSONReader)
		if validationErr != nil {
			return "", validationErr
		}
		if valid {
			return "json", nil
		}
	}
	if mediaTypeErr == nil && isXMLMediaType(mediaType) {
		valid, validationErr := validateHTTPRequestPluginResponseFileWith(ctx, file, validateUTF8Reader)
		if validationErr != nil {
			return "", validationErr
		}
		if valid {
			return "xml", nil
		}
	}
	if isBinaryContentType(contentType) {
		return "binary", nil
	}
	valid, err := validateHTTPRequestPluginResponseFileWith(ctx, file, validateUTF8Reader)
	if err != nil {
		return "", err
	}
	if valid {
		return "text", nil
	}
	return "binary", nil
}

func httpRequestPluginBodyFileSize(ctx context.Context, file *HTTPRequestPluginBodyFile) (int64, error) {
	handle, err := openHTTPRequestPluginBodyFile(ctx, file)
	if err != nil {
		return 0, err
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return 0, fmt.Errorf("inspect response body file: %w", err)
	}
	return info.Size(), nil
}

func validateHTTPRequestPluginResponseFileWith(
	ctx context.Context,
	file *HTTPRequestPluginBodyFile,
	validator func(context.Context, io.Reader) error,
) (bool, error) {
	handle, err := openHTTPRequestPluginBodyFile(ctx, file)
	if err != nil {
		return false, err
	}
	defer handle.Close()
	if err := validator(ctx, handle); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		return false, nil
	}
	return true, nil
}

func openHTTPRequestPluginBodyFile(ctx context.Context, file *HTTPRequestPluginBodyFile) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil || strings.TrimSpace(file.Path) == "" || !filepath.IsAbs(strings.TrimSpace(file.Path)) {
		return nil, errors.New("file-backed plugin body requires an absolute path")
	}
	path := filepath.Clean(strings.TrimSpace(file.Path))
	lstat, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect response body file: %w", err)
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return nil, errors.New("response body file must be a non-symlink regular file")
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open response body file: %w", err)
	}
	info, err := handle.Stat()
	if err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("inspect opened response body file: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(lstat, info) {
		_ = handle.Close()
		return nil, errors.New("response body file changed while opening")
	}
	if err := ctx.Err(); err != nil {
		_ = handle.Close()
		return nil, err
	}
	return handle, nil
}

func materializeHTTPRequestPluginResponseBody(
	ctx context.Context,
	body []byte,
	file *HTTPRequestPluginBodyFile,
	encodeBase64 bool,
) (string, int64, error) {
	if file == nil {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		if encodeBase64 {
			return base64.StdEncoding.EncodeToString(body), int64(len(body)), nil
		}
		return bytes2String(body), int64(len(body)), nil
	}
	if len(body) != 0 {
		return "", 0, errors.New("response body cannot be both inline and file-backed")
	}
	handle, err := openHTTPRequestPluginBodyFile(ctx, file)
	if err != nil {
		return "", 0, err
	}
	defer handle.Close()
	return materializeHTTPRequestPluginResponseReader(ctx, handle, encodeBase64)
}

func materializeHTTPRequestPluginResponseReader(
	ctx context.Context,
	reader io.Reader,
	encodeBase64 bool,
) (string, int64, error) {
	if reader == nil {
		return "", 0, errors.New("response body reader is nil")
	}
	var builder strings.Builder
	contextReader := &contextCheckingReader{ctx: ctx, reader: reader}
	buffer := make([]byte, 64*1024)
	if encodeBase64 {
		encoder := base64.NewEncoder(base64.StdEncoding, &builder)
		written, err := io.CopyBuffer(encoder, contextReader, buffer)
		closeErr := encoder.Close()
		if err != nil {
			return "", written, fmt.Errorf("materialize response body: %w", err)
		}
		if closeErr != nil {
			return "", written, fmt.Errorf("finalize response body encoding: %w", closeErr)
		}
		return builder.String(), written, nil
	}
	written, err := io.CopyBuffer(&builder, contextReader, buffer)
	if err != nil {
		return "", written, fmt.Errorf("materialize response body: %w", err)
	}
	return builder.String(), written, nil
}
