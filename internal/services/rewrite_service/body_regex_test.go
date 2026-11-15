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
