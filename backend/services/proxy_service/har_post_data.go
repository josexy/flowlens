package proxyservice

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Form inspection must not turn streaming HAR exports into unbounded body or
// parameter allocations. Larger or unsupported forms retain the raw body.
const (
	harFormMaxBytes  = 4 << 20
	harFormMaxParams = 1000
)

func writeHARPostData(w io.Writer, request harRequest, body HARBody) error {
	contentType := firstHARHeaderValue(request.Headers, "Content-Type")
	mediaType, attributes, err := mime.ParseMediaType(contentType)
	if err == nil && request.Status == "completed" && body.size() <= harFormMaxBytes &&
		(mediaType == "application/x-www-form-urlencoded" || mediaType == "multipart/form-data") {
		var data []byte
		data, body, err = inspectHARFormBody(body)
		if err != nil {
			return fmt.Errorf("har: read form body: %w", err)
		}
		var params []harPostParam
		if data != nil {
			switch mediaType {
			case "application/x-www-form-urlencoded":
				params = harURLEncodedParams(data)
			case "multipart/form-data":
				params = harMultipartParams(data, attributes["boundary"])
			}
		}
		if params != nil {
			// HAR text and params are mutually exclusive. A binary part uses the
			// same explicit _encoding extension as a raw binary request body.
			return writeHARStructuredPostData(w, contentType, params)
		}
	}
	return writeHARBodyObject(w, contentType, body, "_encoding")
}

// The returned body can always replay the inspected prefix on fallback, even
// when its original reader cannot seek. The caller still owns that reader.
func inspectHARFormBody(body HARBody) ([]byte, HARBody, error) {
	if body.Reader == nil {
		if len(body.Data) > harFormMaxBytes {
			return nil, body, nil
		}
		return body.Data, body, nil
	}
	data, err := io.ReadAll(io.LimitReader(body.Reader, harFormMaxBytes+1))
	if err != nil {
		return nil, body, err
	}
	if len(data) > harFormMaxBytes {
		body.Reader = io.NopCloser(io.MultiReader(bytes.NewReader(data), body.Reader))
		return nil, body, nil
	}
	body.Reader = nil
	body.Data = data
	body.Size = int64(len(data))
	return data, body, nil
}

func harURLEncodedParams(data []byte) []harPostParam {
	params := make([]harPostParam, 0)
	for pair := range strings.SplitSeq(string(data), "&") {
		if pair == "" {
			continue
		}
		if len(params) == harFormMaxParams || strings.Contains(pair, ";") {
			return nil
		}
		name, value, _ := strings.Cut(pair, "=")
		name, nameErr := url.QueryUnescape(name)
		value, valueErr := url.QueryUnescape(value)
		if nameErr != nil || valueErr != nil || !utf8.ValidString(name) || !utf8.ValidString(value) {
			return nil
		}
		params = append(params, harPostParam{Name: name, Value: value})
	}
	return params
}

func harMultipartParams(data []byte, boundary string) []harPostParam {
	if boundary == "" {
		return nil
	}
	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	params := make([]harPostParam, 0)
	for {
		part, err := reader.NextRawPart()
		if err == io.EOF {
			return params
		}
		if err != nil || len(params) == harFormMaxParams {
			return nil
		}
		// HAR params cannot represent arbitrary part headers or duplicate MIME
		// headers. Preserve the raw body instead of dropping that metadata.
		for name, values := range part.Header {
			if len(values) != 1 || (name != "Content-Disposition" && name != "Content-Type") {
				return nil
			}
		}
		disposition, attributes, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name, hasName := attributes["name"]
		if err != nil || disposition != "form-data" || !hasName || !utf8.ValidString(name) {
			return nil
		}
		for name := range attributes {
			if name != "name" && name != "filename" {
				return nil
			}
		}
		value, err := io.ReadAll(part)
		if err != nil {
			// In particular, never export the successfully parsed prefix of a
			// truncated multipart payload as if it were the complete form.
			return nil
		}
		param := harPostParam{
			Name:        name,
			Value:       string(value),
			ContentType: part.Header.Get("Content-Type"),
		}
		if !utf8.ValidString(param.ContentType) {
			return nil
		}
		if filename, present := attributes["filename"]; present {
			if !utf8.ValidString(filename) {
				return nil
			}
			param.FileName = &filename
		}
		if !utf8.Valid(value) {
			param.Value = base64.StdEncoding.EncodeToString(value)
			param.Encoding = "base64"
		}
		params = append(params, param)
	}
}

func writeHARStructuredPostData(w io.Writer, mimeType string, params []harPostParam) error {
	if _, err := io.WriteString(w, "{"); err != nil {
		return err
	}
	first := true
	if err := writeHARJSONField(w, &first, "mimeType", mimeType); err != nil {
		return err
	}
	if err := writeHARJSONField(w, &first, "params", params); err != nil {
		return err
	}
	_, err := io.WriteString(w, "}")
	return err
}
