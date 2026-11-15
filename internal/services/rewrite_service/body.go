package rewriteservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func compileBodyAction(action BodyAction) (*bodyRegex, error) {
	switch action.Mode {
	case "none":
		return nil, nil
	case "replace":
		if len(action.Text) > MaxBodyBytes || !utf8.ValidString(action.Text) {
			return nil, errors.New("replacement body must be UTF-8 and at most 8 MiB")
		}
		return nil, nil
	case "regex":
		if len(action.Pattern) > 16384 || len(action.Replacement) > MaxBodyBytes || !utf8.ValidString(action.Replacement) {
			return nil, errors.New("body regular expression or replacement is too large or replacement is not UTF-8")
		}
		compiled, err := compileBodyRegex(action.Pattern)
		if err != nil {
			return nil, fmt.Errorf("body regular expression: %w", err)
		}
		return compiled, nil
	default:
		return nil, errors.New("invalid body mode")
	}
}

// PreviewBody tests an unsaved regex against sample text without loading rules,
// changing persistent state, or sending traffic.
func (s *RewriteService) PreviewBody(ctx context.Context, action BodyAction, input string) (BodyPreviewResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return BodyPreviewResult{}, err
	}
	if action.Mode != "regex" {
		return BodyPreviewResult{}, errors.New("body preview requires regex mode")
	}
	if len(input) > MaxBodyBytes || !utf8.ValidString(input) || strings.IndexByte(input, 0) >= 0 {
		return BodyPreviewResult{}, errors.New("sample body must be UTF-8 text and at most 8 MiB")
	}
	compiled, err := compileBodyAction(action)
	if err != nil {
		return BodyPreviewResult{}, err
	}
	match := Match{Rule: Rule{Action: Action{Body: action}}, body: compiled}
	output, count, err := match.replaceBody(ctx, input)
	if err != nil {
		return BodyPreviewResult{}, err
	}
	return BodyPreviewResult{MatchCount: count, Output: output}, nil
}

// ReplaceBody uses the regex compiled at save time and bounds both expansion
// and match bookkeeping. It never allocates an unbounded replacement result.
func (m Match) ReplaceBody(ctx context.Context, input string) (string, bool, error) {
	output, _, err := m.replaceBody(ctx, input)
	return output, err == nil && output != input, err
}

func (m Match) replaceBody(ctx context.Context, input string) (string, int, error) {
	if len(input) > MaxBodyBytes || !utf8.ValidString(input) {
		return "", 0, errors.New("body must be UTF-8 and at most 8 MiB")
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	action := m.Rule.Action.Body
	switch action.Mode {
	case "none":
		return input, 0, nil
	case "replace":
		return action.Text, 0, nil
	case "regex":
		if m.body == nil {
			return "", 0, errors.New("body regex is unavailable")
		}
		// Budget index arrays and their slice headers independently of output size.
		// Continuation searches retain one additional capture pair.
		maxMatches := (16 * 1024 * 1024) / (24 + 8*2*(m.body.NumSubexp()+2))
		indexes, err := m.body.findAll(ctx, input, maxMatches+1)
		if err != nil {
			return "", 0, err
		}
		if len(indexes) > maxMatches {
			return "", 0, errors.New("body regular expression exceeds match memory limit")
		}
		if len(indexes) == 0 {
			return input, 0, nil
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
				return "", 0, err
			}
			if err := appendText(input[last:index[0]]); err != nil {
				return "", 0, err
			}
			// Expand one template token at a time so even a single substitution cannot
			// allocate an oversized intermediate (for example $1 repeated many times).
			template := action.Replacement
			for len(template) > 0 {
				if err := ctx.Err(); err != nil {
					return "", 0, err
				}
				dollar := strings.IndexByte(template, '$')
				if dollar < 0 {
					if err := appendText(template); err != nil {
						return "", 0, err
					}
					break
				}
				if err := appendText(template[:dollar]); err != nil {
					return "", 0, err
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
					return "", 0, err
				}
				template = template[end:]
			}
			last = index[1]
		}
		if err := appendText(input[last:]); err != nil {
			return "", 0, err
		}
		result := out.String()
		return result, len(indexes), nil
	default:
		return "", 0, errors.New("unsupported body action")
	}
}
