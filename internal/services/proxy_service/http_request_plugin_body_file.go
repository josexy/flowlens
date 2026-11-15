package proxyservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func validateHTTPRequestPluginRequestBody(ctx context.Context, request *HTTPRequestPluginRequest) error {
	if request == nil {
		return errors.New("plugin request is nil")
	}
	if request.BodyFile == nil {
		return validateHTTPRequestPluginBody(request.Body)
	}
	if request.Body.Text != "" || request.Body.File != nil || len(request.Body.FormData) > 0 || len(request.Body.URLEncoded) > 0 {
		return errors.New("plugin request body cannot be both inline and file-backed")
	}
	kind := string(request.Body.BodyType)
	switch request.Body.BodyType {
	case SendRequestBodyTypeText,
		SendRequestBodyTypeJSON,
		SendRequestBodyTypeXML,
		SendRequestBodyTypeBinary,
		SendRequestBodyTypeFile:
	case "":
		return errors.New("file-backed plugin request body requires a semantic kind")
	default:
		return fmt.Errorf("plugin request body kind %q cannot be file-backed", request.Body.BodyType)
	}
	file := cloneHTTPRequestPluginBodyFile(request.BodyFile)
	if err := validateHTTPRequestPluginBodyFile(ctx, file, kind); err != nil {
		return err
	}
	request.BodyFile = file
	return nil
}

func validateHTTPRequestPluginBodyFile(
	ctx context.Context,
	file *HTTPRequestPluginBodyFile,
	kind string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file == nil || strings.TrimSpace(file.Path) == "" || !filepath.IsAbs(strings.TrimSpace(file.Path)) {
		return errors.New("file-backed plugin body requires an absolute path")
	}
	path := filepath.Clean(strings.TrimSpace(file.Path))
	lstat, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect file-backed plugin body: %w", err)
	}
	if lstat.Mode()&os.ModeSymlink != 0 || !lstat.Mode().IsRegular() {
		return errors.New("file-backed plugin body must be a non-symlink regular file")
	}
	handle, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file-backed plugin body: %w", err)
	}
	defer handle.Close()
	info, err := handle.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened file-backed plugin body: %w", err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(lstat, info) {
		return errors.New("file-backed plugin body changed while opening")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch kind {
	case "text", "xml":
		if err := validateUTF8Reader(ctx, handle); err != nil {
			return fmt.Errorf("plugin %s body file is not valid UTF-8: %w", kind, err)
		}
	case "json":
		if err := validateJSONReader(ctx, handle); err != nil {
			return fmt.Errorf("plugin JSON body file is invalid: %w", err)
		}
	case "binary", "file":
	default:
		return fmt.Errorf("unsupported file-backed plugin body kind %q", kind)
	}
	file.Path = path
	file.Size = info.Size()
	file.ReadOnly = true
	if file.Name == "" {
		file.Name = filepath.Base(path)
	}
	return nil
}

func validateUTF8Reader(ctx context.Context, reader io.Reader) error {
	buffer := make([]byte, 64*1024)
	carry := make([]byte, 0, utf8.UTFMax)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			value := make([]byte, 0, len(carry)+count)
			value = append(value, carry...)
			value = append(value, buffer[:count]...)
			carry = carry[:0]
			for len(value) > 0 {
				if !utf8.FullRune(value) {
					carry = append(carry, value...)
					break
				}
				runeValue, size := utf8.DecodeRune(value)
				if runeValue == utf8.RuneError && size == 1 {
					return errors.New("invalid UTF-8 encoding")
				}
				value = value[size:]
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return readErr
			}
			if len(carry) != 0 {
				return errors.New("truncated UTF-8 encoding")
			}
			return nil
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
}

func validateJSONReader(ctx context.Context, reader io.Reader) error {
	decoder := json.NewDecoder(&contextCheckingReader{ctx: ctx, reader: reader})
	decoder.UseNumber()
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return ctx.Err()
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			if _, ok := key.(string); !ok {
				return errors.New("JSON object key is not a string")
			}
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("JSON array is not closed")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

type contextCheckingReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextCheckingReader) Read(value []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	count, err := r.reader.Read(value)
	if err == nil {
		if contextErr := r.ctx.Err(); contextErr != nil {
			return count, contextErr
		}
	}
	return count, err
}
