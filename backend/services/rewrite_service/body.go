package rewriteservice

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReplaceBody uses the regex compiled at save time and bounds both expansion
// and match bookkeeping. It never allocates an unbounded replacement result.
func (m Match) ReplaceBody(ctx context.Context, input string) (string, bool, error) {
	if len(input) > MaxBodyBytes || !utf8.ValidString(input) {
		return "", false, errors.New("body must be UTF-8 and at most 8 MiB")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	action := m.Rule.Action.Body
	switch action.Mode {
	case "none":
		return input, false, nil
	case "replace":
		return action.Text, action.Text != input, nil
	case "regex":
		if m.body == nil {
			return "", false, errors.New("body regex is unavailable")
		}
		// Budget index arrays and their slice headers independently of output size.
		// Continuation searches retain one additional capture pair.
		maxMatches := (16 * 1024 * 1024) / (24 + 8*2*(m.body.NumSubexp()+2))
		indexes, err := m.body.findAll(ctx, input, maxMatches+1)
		if err != nil {
			return "", false, err
		}
		if len(indexes) > maxMatches {
			return "", false, errors.New("body regular expression exceeds match memory limit")
		}
		if len(indexes) == 0 {
			return input, false, nil
		}
		var out strings.Builder
		last := 0
		appendText := func(text string) error {
			if out.Len()+len(text) > MaxBodyBytes {
				return errors.New("rewritten body exceeds 8 MiB")
			}
			out.WriteString(text)
			return nil
		}
		for _, index := range indexes {
			if err := ctx.Err(); err != nil {
				return "", false, err
			}
			if err := appendText(input[last:index[0]]); err != nil {
				return "", false, err
			}
			// Expand one template token at a time so even a single substitution cannot
			// allocate an oversized intermediate (for example $1 repeated many times).
			template := action.Replacement
			for len(template) > 0 {
				if err := ctx.Err(); err != nil {
					return "", false, err
				}
				dollar := strings.IndexByte(template, '$')
				if dollar < 0 {
					if err := appendText(template); err != nil {
						return "", false, err
					}
					break
				}
				if err := appendText(template[:dollar]); err != nil {
					return "", false, err
				}
				template = template[dollar:]
				end := 1
				if len(template) > 1 && template[1] == '$' {
					end = 2
				} else {
					cursor := 1
					braced := cursor < len(template) && template[cursor] == '{'
					if braced {
						cursor++
					}
					nameStart := cursor
					for cursor < len(template) {
						r, size := utf8.DecodeRuneInString(template[cursor:])
						if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
							break
						}
						cursor += size
					}
					if cursor > nameStart {
						if !braced {
							end = cursor
						} else if cursor < len(template) && template[cursor] == '}' {
							end = cursor + 1
						}
					}
				}
				expanded := m.body.ExpandString(nil, template[:end], input, index)
				if err := appendText(string(expanded)); err != nil {
					return "", false, err
				}
				template = template[end:]
			}
			last = index[1]
		}
		if err := appendText(input[last:]); err != nil {
			return "", false, err
		}
		result := out.String()
		return result, result != input, nil
	default:
		return "", false, errors.New("unsupported body action")
	}
}
