package logic

import (
	"errors"
	"math"
	"testing"

	apicollectionservice "github.com/josexy/flowlens/backend/services/api_collection_service"
	proxyservice "github.com/josexy/flowlens/backend/services/proxy_service"
)

func savedHTTP() *apicollectionservice.SavedHTTPRequest {
	return &apicollectionservice.SavedHTTPRequest{Method: "GET", URL: "https://example.test/", BodyType: "none", ProxyMode: "none"}
}

func httpTab(state *HTTPEditorState) *WorkspaceTab {
	return &WorkspaceTab{Key: "http-request:1", Type: TabTypeHTTPRequest, Closable: true, HTTP: state}
}

func wsTab(state *WebSocketClientState) *WorkspaceTab {
	return &WorkspaceTab{Key: "websocket-client:1", Type: TabTypeWebSocketClient, Closable: true, WebSocket: state}
}

func TestTrafficEditRejectsMissingBodiesAndKeepsEmptyOnes(t *testing.T) {
	entry := &TrafficEntry{ID: 1, Method: "POST", URL: "https://example.test/", Host: "example.test", Request: &proxyservice.HTTPMessage{HeaderFields: []HeaderField{}}}
	body := &proxyservice.TrafficBodyView{RequestBodyUnavailable: true}
	if _, err := HTTPEditorStateFromTraffic(DraftHistoryEdit, entry, body, ""); !errors.Is(err, ErrRequestBodyUnavailable) {
		t.Fatal(err)
	}
	body.RequestBodyUnavailable = false
	state, err := HTTPEditorStateFromTraffic(DraftHistoryEdit, entry, body, "")
	if err != nil || state.RequestBodyText != "" {
		t.Fatal(err)
	}
}

func TestTLSVerificationIsPerTabAndSaved(t *testing.T) {
	state := BuildEmptyHTTPEditorState(DraftNew)
	other := BuildEmptyHTTPEditorState(DraftNew)
	tab := httpTab(state)
	if state.Settings.SkipVerifyTLS || HasMeaningfulRequestDraft(tab) {
		t.Fatal("default")
	}
	initial := CreateSnapshotForTab(tab)
	state.Settings.SkipVerifyTLS = true
	if other.Settings.SkipVerifyTLS || !HasMeaningfulRequestDraft(tab) || CreateSnapshotForTab(tab) == initial {
		t.Fatal("per tab")
	}
	for _, skip := range []bool{true, false} {
		state.Settings.SkipVerifyTLS = skip
		saved := BuildSavedHTTPRequest(state)
		if saved.SkipVerifyTLS != skip || BuildHTTPEditorStateFromSaved(saved, "Saved").Settings.SkipVerifyTLS != skip {
			t.Fatal("restore")
		}
		other.Settings.SkipVerifyTLS = !skip
		ApplySavedHTTPRequest(other, saved, "Saved")
		if other.Settings.SkipVerifyTLS != skip {
			t.Fatal("apply")
		}
	}
	if CreateSnapshotForTab(tab) != initial {
		t.Fatal("snapshot")
	}
}

func TestLegacyAndTrafficDraftsVerifyTLSAndDisableRedirects(t *testing.T) {
	legacy := savedHTTP()
	restored := BuildHTTPEditorStateFromSaved(legacy, "Legacy")
	if restored.Settings.SkipVerifyTLS || restored.Settings.MaxRedirects != 0 {
		t.Fatal("legacy")
	}
	restored.Settings.SkipVerifyTLS, restored.Settings.MaxRedirects = true, 12
	ApplySavedHTTPRequest(restored, legacy, "Legacy")
	if restored.Settings.SkipVerifyTLS || restored.Settings.MaxRedirects != 0 {
		t.Fatal("reapply")
	}
	draft, _ := HTTPEditorStateFromTraffic(DraftCaptureEdit, &TrafficEntry{ID: 1, Method: "GET", URL: "https://example.test/", Request: &proxyservice.HTTPMessage{HeaderFields: []HeaderField{}}}, nil, "")
	if draft.Settings.SkipVerifyTLS || draft.Settings.MaxRedirects != 0 {
		t.Fatal("traffic")
	}
}

func TestRedirectLimitsSavedAndDirty(t *testing.T) {
	state := BuildEmptyHTTPEditorState(DraftNew)
	other := BuildEmptyHTTPEditorState(DraftNew)
	tab := httpTab(state)
	initial := CreateSnapshotForTab(tab)
	for _, limit := range []int{1, 12, 0} {
		state.Settings.MaxRedirects = limit
		if other.Settings.MaxRedirects != 0 || HasMeaningfulRequestDraft(tab) != (limit > 0) || (CreateSnapshotForTab(tab) == initial) != (limit == 0) {
			t.Fatalf("limit %d", limit)
		}
		saved := BuildSavedHTTPRequest(state)
		restored := BuildHTTPEditorStateFromSaved(saved, "Saved")
		if saved.MaxRedirects != limit || restored.Settings.MaxRedirects != limit {
			t.Fatal("saved")
		}
	}
}

func TestRedirectInputNormalization(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -1, math.Inf(-1)} {
		if NormalizeHTTPMaxRedirects(value) != 0 {
			t.Fatal(value)
		}
	}
	if NormalizeHTTPMaxRedirects(1.8) != 1 || NormalizeHTTPMaxRedirects(12) != 12 {
		t.Fatal("whole")
	}
}

func TestWebSocketTLSIsPerTabAndSaved(t *testing.T) {
	state := BuildEmptyWebSocketClientState(DraftNew)
	other := BuildEmptyWebSocketClientState(DraftNew)
	tab := wsTab(state)
	if HasMeaningfulRequestDraft(tab) {
		t.Fatal("default")
	}
	initial := CreateSnapshotForTab(tab)
	state.Settings.SkipVerifyTLS = true
	if other.Settings.SkipVerifyTLS || !HasMeaningfulRequestDraft(tab) || CreateSnapshotForTab(tab) == initial {
		t.Fatal("per tab")
	}
	for _, skip := range []bool{true, false} {
		state.Settings.SkipVerifyTLS = skip
		saved := BuildSavedWebSocketRequest(state)
		if BuildWebSocketClientStateFromSaved(saved, "Saved").Settings.SkipVerifyTLS != skip {
			t.Fatal("restore")
		}
		other.Settings.SkipVerifyTLS = !skip
		ApplySavedWebSocketRequest(other, saved, "Saved")
		if other.Settings.SkipVerifyTLS != skip {
			t.Fatal("apply")
		}
	}
	if CreateSnapshotForTab(tab) != initial {
		t.Fatal("snapshot")
	}
}

func TestWebSocketLegacyAndTrafficVerifyTLS(t *testing.T) {
	legacy := &apicollectionservice.SavedWebSocketRequest{URL: "wss://example.test/", DraftType: "text", ProxyMode: "none"}
	restored := BuildWebSocketClientStateFromSaved(legacy, "Legacy")
	if restored.Settings.SkipVerifyTLS {
		t.Fatal("legacy")
	}
	draft := WebSocketClientStateFromTraffic(DraftHistoryEdit, &TrafficEntry{ID: 1, URL: "wss://example.test/", Request: &proxyservice.HTTPMessage{HeaderFields: []HeaderField{}}}, "")
	if draft.Settings.SkipVerifyTLS {
		t.Fatal("traffic")
	}
}

func TestInlineScriptSavedDisabled(t *testing.T) {
	state := BuildEmptyHTTPEditorState(DraftNew)
	tab := httpTab(state)
	if state.PluginsEnabled || state.InlineScriptEnabled || HasMeaningfulRequestDraft(tab) {
		t.Fatal("default")
	}
	state.InlineScriptSource = "def onRequest(context, request):\n    return request\n"
	if !HasMeaningfulRequestDraft(tab) {
		t.Fatal("dirty")
	}
	saved := BuildSavedHTTPRequest(state)
	restored := BuildHTTPEditorStateFromSaved(saved, "Saved")
	if restored.InlineScriptSource != state.InlineScriptSource || restored.InlineScriptEnabled || restored.PluginsEnabled {
		t.Fatal("restore")
	}
}

func TestLegacyAndEmptySavedScripts(t *testing.T) {
	if BuildHTTPEditorStateFromSaved(savedHTTP(), "Legacy").InlineScriptSource != DefaultHTTPRequestPythonScript {
		t.Fatal("legacy")
	}
	empty := ""
	request := savedHTTP()
	request.InlineScriptSource = &empty
	if state := BuildHTTPEditorStateFromSaved(request, "Empty"); state.InlineScriptSource != "" || state.InlineScriptEnabled {
		t.Fatal("empty")
	}
}

func TestDraftCacheCleanupClearsOnlyManagedReferences(t *testing.T) {
	http := BuildEmptyHTTPEditorState(DraftCaptureEdit)
	http.RequestBodyType, http.RequestBodyText = BodyTypeFile, "keep hidden text body"
	http.RequestBodyFile = &RequestFileValue{Path: "c:/FLOWLENS/request-draft-cache/recovered/body.bin", Name: "body.bin", Size: 10}
	http.RequestBodyFormData = []FormDataItem{
		{ID: "cached-row", Enabled: true, Name: "cached", ItemType: "file", Value: "keep row value", File: &RequestFileValue{Path: `C:\FlowLens\request-draft-cache\multipart\upload.bin`}},
		{ID: "managed-row", Enabled: true, Name: "managed", ItemType: "file", File: &RequestFileValue{Path: "C:/FlowLens/api-collection-files/managed.bin"}},
		{ID: "external-row", Enabled: true, Name: "external", ItemType: "file", File: &RequestFileValue{Path: "C:/FlowLens/request-draft-cache-old/external.bin"}},
	}
	ws := BuildEmptyWebSocketClientState(DraftHistoryEdit)
	ws.DraftType, ws.DraftText = "binary-file", "keep websocket text draft"
	ws.DraftFile = &RequestFileValue{Path: "C:/FlowLens/request-draft-cache/../request-draft-cache/ws.bin"}
	tabs := []*WorkspaceTab{httpTab(http), wsTab(ws)}
	if got := ClearRequestDraftCacheFileReferences(tabs, `C:\FlowLens\request-draft-cache`); got != 3 {
		t.Fatal(got)
	}
	if http.RequestBodyFile != nil || http.RequestBodyText != "keep hidden text body" || http.RequestBodyFormData[0].File != nil || http.RequestBodyFormData[0].Value != "keep row value" {
		t.Fatalf("%+v", http)
	}
	if http.RequestBodyFormData[1].File == nil || http.RequestBodyFormData[2].File == nil {
		t.Fatal("kept files")
	}
	if ws.DraftFile != nil || ws.DraftType != "binary-file" || ws.DraftText != "keep websocket text draft" {
		t.Fatalf("%+v", ws)
	}
}

func TestQueryRows(t *testing.T) {
	rows := ParseRequestQueryRows("https://a.test/p?x=1&y=a+b%21#frag")
	if len(rows) != 2 || rows[1].Value != "a b!" {
		t.Fatalf("%+v", rows)
	}
	if got := ReplaceRequestURLQuery("https://a.test/p?old=1#frag", []EditableRow{{Key: " k ", Value: "a b~*", Enabled: true}, {Key: "off", Value: "x"}}); got != "https://a.test/p?k=a+b%7E*#frag" {
		t.Fatal(got)
	}
	if got := ReplaceRequestURLQuery("https://a.test/p?old=1", nil); got != "https://a.test/p" {
		t.Fatal(got)
	}
	if CountRequestQueryRows([]EditableRow{{Key: "a", Enabled: true}, {Key: " "}, {Key: "b"}}) != 1 {
		t.Fatal("count")
	}
	if got := ParseRequestQueryRows("https://a.test/#?x=1"); len(got) != 1 || got[0].Key != "" {
		t.Fatalf("%+v", got)
	}
}

func TestRestoredURLKeepsItsQuery(t *testing.T) {
	request := savedHTTP()
	request.URL = "https://a.test/p"
	request.Params = []*apicollectionservice.SavedKeyValue{{Key: "q", Value: "1", Enabled: true}}
	if got := BuildHTTPEditorStateFromSaved(request, "x").URL; got != "https://a.test/p?q=1" {
		t.Fatal(got)
	}
	request.URL = "https://a.test/p?z=2"
	if got := BuildHTTPEditorStateFromSaved(request, "x").URL; got != "https://a.test/p?z=2" {
		t.Fatal(got)
	}
}

func TestBodyRecovery(t *testing.T) {
	state := BuildEmptyHTTPEditorState(DraftCaptureEdit)
	ApplyRequestBodyRecovery(state, &proxyservice.RequestBodyRecoveryResult{BodyType: BodyTypeURLEncoded, URLEncoded: []*proxyservice.SendRequestURLEncodedItem{{Enabled: true, Name: "a", Value: "1"}}})
	if state.RequestBodyType != BodyTypeURLEncoded || state.RequestBodyURLEncoded[0].Key != "a" || len(state.RequestBodyFormData) != 1 {
		t.Fatalf("%+v", state)
	}
}

func TestDeriveTabTitle(t *testing.T) {
	for input, want := range map[string]string{"https://a.test:8/x": "a.test", "a.test/x": "a.test", "": "", "/only": ""} {
		if got := DeriveRequestTabTitle(input); got != want {
			t.Errorf("%q: %q", input, got)
		}
	}
}
