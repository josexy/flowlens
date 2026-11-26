package proxyservice

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unsafe"
)

func TestHistoryBodyViewTakesOwnedPayload(t *testing.T) {
	body := strings.Repeat("body\x00\xff", 1<<16)
	record := hbinEntryBodyRecord(t, &trafficBodyViewInner{
		ResponseBodyReader: io.NopCloser(strings.NewReader(body)), ResponseBodySize: int64(len(body)), ResponseBodyBinary: true,
	})
	decoded, err := hbinReadEntryBody(bytes.NewReader(record), hbinVersionCurrent)
	if err != nil {
		t.Fatal(err)
	}
	owned := decoded.ResponseBodyReader.(*hbinBodyReader)
	address := &owned.data[0]
	view, err := convertToTrafficBodyView(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if view.ResponseBody != body || !view.ResponseBodyBinary || unsafe.StringData(view.ResponseBody) != address {
		t.Fatal("view copied or changed the owned payload")
	}
	if owned.data != nil || owned.Len() != 0 {
		t.Fatal("reader retained transferred data")
	}
	decoded.closeReqBodyReaderSafely()
	decoded.closeRspBodyReaderSafely()
	if view.ResponseBody != body {
		t.Fatal("closing readers invalidated the view")
	}
}

func TestBodyViewStillCopiesMutableCaptureBytes(t *testing.T) {
	data := []byte("original capture")
	view, err := convertToTrafficBodyView(&trafficBodyViewInner{ResponseBodyReader: io.NopCloser(bytes.NewReader(data)), ResponseBodySize: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	if view.ResponseBody != "original capture" {
		t.Fatal("capture mutation changed published body")
	}
}

func TestOwnedHistoryReaderTransfersOnlyUnreadBytes(t *testing.T) {
	data := []byte("abcdef")
	r := &hbinBodyReader{Reader: bytes.NewReader(data), data: data}
	_, _ = r.Read(make([]byte, 2))
	got, err := readBodyOfSize(r, 6)
	if err != nil || string(got) != "cdef" {
		t.Fatalf("remaining body = %q, %v", got, err)
	}
}
