package rewriteservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPreviewBodyMatchesLiveReplacement(t *testing.T) {
	// The preview must also work for an unsaved draft, without a database or load.
	s := &RewriteService{}
	for _, tt := range []struct {
		name, pattern, replacement, input, output string
		count                                     int
	}{
		{"exact whitespace", `"accept-encoding": "([^"]*)"`, `"encoding": "${1}"`, `{"accept-encoding": "gzip"}`, `{"encoding": "gzip"}`, 1},
		{"missing whitespace", `"accept-encoding": "([^"]*)"`, `$1`, `{"accept-encoding":"gzip"}`, `{"accept-encoding":"gzip"}`, 0},
		{"optional whitespace", `(?i)"accept-encoding"\s*:\s*"([^"]*)"`, `"encoding": "${1}"`, `{"Accept-Encoding":"gzip"}`, `{"encoding": "gzip"}`, 1},
		{"all matches and groups", `(foo)(\d+)`, `${2}:$1:$$`, `foo42 foo99`, `42:foo:$ 99:foo:$`, 2},
		{"matched but unchanged", `(foo)`, `$1`, `foo foo`, `foo foo`, 2},
		{"empty result", `foo`, ``, `foo`, ``, 1},
		{"empty sample", `^$`, `empty`, ``, `empty`, 1},
		{"empty pattern", ``, `-`, `ab`, `-a-b-`, 3},
		{"unicode named group", `(?P<word>世界)`, `${word}!`, `你好世界`, `你好世界!`, 1},
		{"multiline", `(?m)^a$`, `b`, "a\na", "b\nb", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			action := BodyAction{Mode: "regex", Pattern: tt.pattern, Replacement: tt.replacement}
			got, err := s.PreviewBody(t.Context(), action, tt.input)
			if err != nil || got.MatchCount != tt.count || got.Output != tt.output {
				t.Fatalf("preview = %+v, %v; want count=%d output=%q", got, err, tt.count, tt.output)
			}
			rule := testRule()
			rule.Action = Action{Version: ConfigVersion, Type: ActionResponse, Body: action}
			compiled, err := compileRule(rule)
			if err != nil {
				t.Fatal(err)
			}
			live := Match{Rule: rule, body: compiled.body}
			output, changed, err := live.ReplaceBody(t.Context(), tt.input)
			if err != nil || output != got.Output || changed != (tt.input != got.Output) {
				t.Fatalf("live result differs: %q, %v, %v", output, changed, err)
			}
		})
	}
	if s.snapshot.Load() != nil {
		t.Fatal("preview published a rule snapshot")
	}
}

func TestPreviewBodyRejectsInvalidOrOversizedText(t *testing.T) {
	s := &RewriteService{}
	for _, tt := range []struct {
		name, mode, pattern, replacement, input string
	}{
		{"non-regex mode", "replace", "a", "b", "a"},
		{"invalid regex", "regex", "(", "b", "a"},
		{"unsupported lookbehind", "regex", "(?<=a)b", "c", "ab"},
		{"pattern limit", "regex", strings.Repeat("a", 16385), "b", "a"},
		{"replacement limit", "regex", "a", strings.Repeat("b", MaxBodyBytes+1), "a"},
		{"input limit", "regex", "a", "b", strings.Repeat("a", MaxBodyBytes+1)},
		{"output limit", "regex", "a", strings.Repeat("b", MaxBodyBytes), "aa"},
		{"invalid UTF-8 input", "regex", "a", "b", "\xff"},
		{"invalid UTF-8 replacement", "regex", "a", "\xff", "a"},
		{"binary input", "regex", ".", "b", "a\x00b"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.PreviewBody(t.Context(), BodyAction{Mode: tt.mode, Pattern: tt.pattern, Replacement: tt.replacement}, tt.input)
			if err == nil || got != (BodyPreviewResult{}) {
				t.Fatalf("invalid preview returned a result: %+v, %v", got, err)
			}
		})
	}
	input := strings.Repeat("a", MaxBodyBytes)
	got, err := s.PreviewBody(t.Context(), BodyAction{Mode: "regex", Pattern: "z"}, input)
	if err != nil || got.MatchCount != 0 || got.Output != input {
		t.Fatalf("exact input limit rejected: %v", err)
	}
}

func TestPreviewBodyCancellation(t *testing.T) {
	s := &RewriteService{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.PreviewBody(ctx, BodyAction{Mode: "regex", Pattern: "a"}, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preview: %v", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	_, err := s.PreviewBody(ctx, BodyAction{Mode: "regex", Pattern: `[ab]{1000}z`}, strings.Repeat("a", MaxBodyBytes))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preview did not honor caller deadline: %v", err)
	}
}
