package rewriteservice

import (
	"context"
	"errors"
	"reflect"
	"regexp"
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

func TestBodyRegexFindAllPreservesGoSemantics(t *testing.T) {
	patterns := []string{
		``, `a`, `(a)`, `(?P<x>needle)`, `needle`, `a*`, `a*?`, `a+|b`, `a|ab`, `ab|a`, `(a*)(b?)`,
		`^`, `$`, `\A`, `\z`, `^a|b$`, `(?m)^.*$`, `(?m)^$`, `(?s).*`,
		`\b`, `\B`, `\ba\b`, `(?i)a`, `(?i:a)(?-i:b)`, `(?P<x>a)|(b)`,
		`(é*)(世?)`, `é|世界`, `.`, `.*?`, `[[:alpha:]]+`, `\Q(a`,
	}
	inputs := []string{"", "a", "b", "ab", "aba", "aaab", " a ", "a\nb\n", "\n\n", "é世界aé", "éé世界", "needle needle", "(a(a"}
	// Exercise byte/rune offsets and consecutive empty matches across varying
	// input contexts without using wall-clock performance assertions.
	for _, first := range []string{"a", "b", "é", "世", " ", "\n"} {
		for _, second := range []string{"a", "b", "é", "世", " ", "\n"} {
			inputs = append(inputs, first+second+first)
		}
	}
	for _, pattern := range patterns {
		r, err := compileBodyRegex(pattern)
		if err != nil {
			t.Fatal(err)
		}
		standard := regexp.MustCompile(pattern)
		for _, input := range inputs {
			for _, limit := range []int{1, 2, 100} {
				got, err := r.findAll(t.Context(), input, limit)
				want := standard.FindAllStringSubmatchIndex(input, limit)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("pattern=%q input=%q limit=%d: got %v, %v; want %v", pattern, input, limit, got, err, want)
				}
			}
		}
	}
}

func TestBodyRegexCancellationInterruptsScan(t *testing.T) {
	r, err := compileBodyRegex(`[ab]{1000}z`)
	if err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("a", MaxBodyBytes)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = r.findAll(ctx, input, 100)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("scan did not propagate cancellation: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("scan retained execution after deadline: %v", elapsed)
	}
}

func TestBodyRegexPreservesGoNestingLimit(t *testing.T) {
	patterns := []string{
		strings.Repeat("(", 998) + "a" + strings.Repeat(")", 998),
		strings.Repeat("(", 999) + "a" + strings.Repeat(")", 999),
		strings.Repeat("(a", 500) + "b" + strings.Repeat(")", 500),
		`(?:\ba` + strings.Repeat("(?:a", 499) + "b" + strings.Repeat(")*", 500),
	}
	for _, pattern := range patterns {
		standard := regexp.MustCompile(pattern)
		r, err := compileBodyRegex(pattern)
		if err != nil {
			t.Fatalf("valid Go expression rejected: %v", err)
		}
		got, err := r.findAll(t.Context(), "aba", 10)
		if want := standard.FindAllStringSubmatchIndex("aba", 10); err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("deep expression changed matches: got %v, %v; want %v", got, err, want)
		}
	}
}

func TestBodyRegexExtremeNestingUsesBoundedFallback(t *testing.T) {
	pattern := `(?:\ba` + strings.Repeat("(?:a", 499) + "b" + strings.Repeat(")*", 500)
	r, err := compileBodyRegex(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if r.fallbackCost == 0 {
		t.Fatal("test must exercise the maximum-depth fallback")
	}
	if _, err = r.findAll(t.Context(), strings.Repeat("a", MaxBodyBytes), 100); err == nil || !strings.Contains(err.Error(), "execution budget") {
		t.Fatalf("extreme program executed without a work budget: %v", err)
	}
}
