package rewriteservice

import (
	"context"
	"errors"
	"io"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode/utf8"
)

// bodyRegex uses the standard Go engine with a cancellable RuneReader. The
// continuation expression consumes the preceding rune before searching, so
// anchors and word boundaries still see their original input context.
type bodyRegex struct {
	*regexp.Regexp
	continuation *regexp.Regexp
	prefix       string
	literal      bool
	fallbackCost uint64
}

const maxBodyRegexFallbackWork = 16 * 1024 * 1024

func compileBodyRegex(pattern string) (*bodyRegex, error) {
	parsed, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return nil, err
	}
	// Normalize syntax before enclosing it: an unterminated \Q quote must not
	// consume our closing parenthesis, and inline flags must remain scoped.
	canonical := parsed.String()
	original, err := regexp.Compile(canonical)
	if err != nil {
		return nil, err
	}
	prefix, literal := original.LiteralPrefix()
	literal = literal && original.NumSubexp() == 0
	r := &bodyRegex{Regexp: original, prefix: prefix, literal: literal}
	if !needsBodyRegexContext(parsed) {
		// Without left-context assertions, the original expression can search
		// a suffix directly, retaining its capture numbers and depth limits.
		return r, nil
	}
	r.continuation, err = regexp.Compile(`\A(?s:.)(?s:.*?)(` + canonical + `)`)
	if err != nil {
		// A valid expression already at Go's nesting/size limit may not fit
		// an extra context wrapper. Preserve syntax acceptance; execute these
		// rare cases only with a small, conservative whole-search work bound.
		program, compileErr := syntax.Compile(parsed.Simplify())
		if compileErr != nil {
			return nil, compileErr
		}
		r.fallbackCost = uint64(len(program.Inst))
		for _, inst := range program.Inst {
			r.fallbackCost += uint64(len(inst.Rune))
		}
		r.fallbackCost *= uint64(2 * (original.NumSubexp() + 1))
	}
	return r, nil
}

func needsBodyRegexContext(expression *syntax.Regexp) bool {
	switch expression.Op {
	case syntax.OpBeginText, syntax.OpBeginLine, syntax.OpWordBoundary, syntax.OpNoWordBoundary:
		return true
	}
	for _, sub := range expression.Sub {
		if needsBodyRegexContext(sub) {
			return true
		}
	}
	return false
}

type bodyRegexReader struct {
	done  <-chan struct{}
	input string
	pos   int
}

func (r *bodyRegexReader) ReadRune() (rune, int, error) {
	select {
	case <-r.done:
		return 0, 0, io.EOF
	default:
	}
	if r.pos >= len(r.input) {
		return 0, 0, io.EOF
	}
	value, size := utf8.DecodeRuneInString(r.input[r.pos:])
	r.pos += size
	return value, size, nil
}

func (r *bodyRegex) findAll(ctx context.Context, input string, limit int) ([][]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.fallbackCost != 0 {
		// FindAll can rescan suffixes (e.g. a.*z|a). Budget both the maximum
		// number of searches and bytes per search, plus NFA/capture work.
		size := uint64(len(input)) + 1
		if r.fallbackCost > maxBodyRegexFallbackWork/size/size {
			return nil, errors.New("body regular expression exceeds execution budget")
		}
		matches := r.FindAllStringSubmatchIndex(input, limit)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return matches, nil
	}
	var matches [][]int
	previousEnd := -1
	for pos := 0; pos <= len(input) && len(matches) < limit; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		search := pos
		if r.prefix != "" {
			// RuneReader cannot use regexp's string-prefix acceleration. Keep
			// that fast path explicitly, particularly for large no-match bodies.
			offset := strings.Index(input[pos:], r.prefix)
			if offset < 0 {
				break
			}
			search += offset
		}
		var index []int
		if r.literal && r.prefix != "" {
			index = []int{search, search + len(r.prefix)}
		} else {
			base := search
			expression := r.Regexp
			withContext := search > 0 && r.continuation != nil
			if withContext {
				_, size := utf8.DecodeLastRuneInString(input[:search])
				base -= size
				expression = r.continuation
			}
			index = expression.FindReaderSubmatchIndex(&bodyRegexReader{done: ctx.Done(), input: input[base:]})
			// Reader cancellation looks like EOF to regexp and could create a
			// spurious end-anchor match. Never accept it after cancellation.
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if index == nil {
				break
			}
			if withContext {
				index = index[2:] // outer capture is the original whole match
			}
			if base > 0 {
				for i, value := range index {
					if value >= 0 {
						index[i] += base
					}
				}
			}
		}
		accept := true
		if index[1] == pos {
			// Match Go's FindAll semantics: ignore an empty match abutting the
			// preceding match and advance by a full UTF-8 rune, including EOF.
			accept = index[0] != previousEnd
			if pos < len(input) {
				_, size := utf8.DecodeRuneInString(input[pos:])
				pos += size
			} else {
				pos++
			}
		} else {
			pos = index[1]
		}
		previousEnd = index[1]
		if accept {
			matches = append(matches, index)
		}
	}
	return matches, ctx.Err()
}
