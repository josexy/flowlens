package pythonapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func names(source string) []string {
	var result []string
	for _, item := range Completions(source, len(source)).Items {
		result = append(result, item.Name)
	}
	return result
}

func TestCompletionResolvesHookArgumentsAndNestedMembers(t *testing.T) {
	cases := []struct {
		source string
		want   []string
	}{
		{"def onRequest(context, request):\n    request.", []string{"method", "url", "scheme", "host", "port", "path", "queries", "content_type", "headers", "body"}},
		{"def onRequest(ctx, req):\n    req.headers.", []string{"get", "get_all", "set", "add", "remove", "clear"}},
		{"def onRequest(ctx, req):\n    req.body.", []string{"kind", "value", "write_file"}},
		{"def onRequest(ctx, req):\n    req.queries.", []string{"get", "get_all", "set", "add", "remove", "clear", "to_string"}},
		{"def onResponse(ctx, res):\n    res.request.headers.", []string{"get", "get_all"}},
		{"def onResponse(ctx, res):\n    res.request.queries.", []string{"get", "get_all", "to_string"}},
		{"def onResponse(ctx, res):\n    res.request.body.", []string{"kind", "value", "write_file"}},
		{"def onResponse(ctx, res):\n    res.", []string{"code", "protocol", "status_text", "content_type", "headers", "trailers", "body", "request"}},
		{"def onResponse(ctx, res):\n    ctx.log.", []string{"debug", "info", "warning", "error"}},
		{"def helper(req: Request):\n    headers = req.headers\n    headers.", []string{"get", "get_all", "set", "add", "remove", "clear"}},
		{"FileDescriptor.", []string{"from_file"}},
		{"context.params.", []string{"get", "keys", "items", "values"}},
		{"context.shared.", []string{"get", "keys", "items", "values", "setdefault", "update", "pop", "clear"}},
	}
	for _, testCase := range cases {
		if got := names(testCase.source); !slices.Equal(got, testCase.want) {
			t.Fatalf("%q: %v", testCase.source, got)
		}
	}
	if !slices.Contains(names("Bo"), "Body") {
		t.Fatal(names("Bo"))
	}
}

func TestCompletionSignatureAndHover(t *testing.T) {
	completion := Completions("request.headers.se", len("request.headers.se"))
	if completion.ReplaceLength != 2 || completion.Items[0].Name != "set" || completion.Items[0].InsertText != "set('${1:name}', '${2:value}')" {
		t.Fatalf("%+v", completion)
	}
	call := "request.headers.set('X-Test', "
	signature := SignatureAt(call, len(call))
	if signature == nil || signature.Label != "Headers.set(name: str, value: str) -> None" || signature.ActiveParameter != 1 {
		t.Fatalf("%+v", signature)
	}
	if got := SignatureAt("Body(", 5); got == nil || got.Label != `Body(kind: str = "none", value: Any = None)` {
		t.Fatalf("%+v", got)
	}
	if got := SignatureAt("MultipartPart(", 14); got == nil || got.Label != `MultipartPart(name: str, value: str = "", file: FileDescriptor | None = None, enabled: bool = True, filename: str = "")` {
		t.Fatalf("%+v", got)
	}
	source := "response.body"
	hover := HoverAt(source, strings.Index(source, "body")+1)
	if hover == nil || hover.Signature != "(property) Response.body: Body" || !strings.Contains(hover.Documentation, "SSE") {
		t.Fatalf("%+v", hover)
	}
	if HoverAt("nothing.here", 2) != nil {
		t.Fatal("unknown names have no hover")
	}
	if got := SignatureAt("request.headers.set('a,b', ", 27); got == nil || got.ActiveParameter != 1 {
		t.Fatalf("commas in strings do not count: %+v", got)
	}
}

func TestSystemPromptsMatchTheCatalog(t *testing.T) {
	for _, scope := range []string{ScopeGlobal, ScopeInline} {
		for _, locale := range []string{"en", "zh-Hans"} {
			want, err := os.ReadFile(filepath.Join("data", "testdata", "prompt-"+scope+"-"+locale+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			if got := SystemPrompt(scope, locale); got != string(want) {
				for index := range min(len(got), len(want)) {
					if got[index] != want[index] {
						t.Fatalf("%s %s differs at %d: %q vs %q", scope, locale, index, got[max(0, index-40):min(len(got), index+40)], string(want[max(0, index-40):min(len(want), index+40)]))
					}
				}
				t.Fatalf("%s %s: lengths %d vs %d", scope, locale, len(got), len(want))
			}
		}
	}
}

func TestGroupSearchMatchesTheCatalog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("data", "testdata", "groups-search.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want []struct {
		Name    string `json:"name"`
		Entries []struct {
			Signature string `json:"signature"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	got := Groups("en", "set")
	if len(got) != len(want) {
		t.Fatalf("%d groups, want %d", len(got), len(want))
	}
	for index := range got {
		if got[index].Name != want[index].Name || len(got[index].Entries) != len(want[index].Entries) {
			t.Fatalf("%s %d vs %s %d", got[index].Name, len(got[index].Entries), want[index].Name, len(want[index].Entries))
		}
		for position := range got[index].Entries {
			if got[index].Entries[position].Signature != want[index].Entries[position].Signature {
				t.Fatal(got[index].Entries[position].Signature)
			}
		}
	}
	if len(Groups("en", "no such thing anywhere")) != 0 {
		t.Fatal("no results")
	}
}

func TestEveryLanguageDescribesEveryEntry(t *testing.T) {
	for _, locale := range []string{"en", "zh-Hans"} {
		for _, group := range Groups(locale, "") {
			if len(group.Entries) == 0 {
				t.Fatal(group.Name)
			}
			for _, entry := range group.Entries {
				if entry.Description == "" || entry.Signature == "" {
					t.Fatalf("%s %+v", group.Name, entry)
				}
			}
		}
	}
}
