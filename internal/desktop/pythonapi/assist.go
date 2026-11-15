package pythonapi

import (
	"regexp"
	"slices"
	"strings"
)

// Completion is an item the editors offer.
type Completion struct {
	Name, Kind, Detail, InsertText, Documentation string
	Snippet                                       bool
}

// CompletionResult is what to offer at a position.
type CompletionResult struct {
	ReplaceLength int
	Items         []Completion
}

// Signature describes the call the caret is in.
type Signature struct {
	Label, Documentation string
	Parameters           []string
	ActiveParameter      int
}

// Hover describes the name under the pointer.
type Hover struct {
	Signature, Documentation string
}

var (
	memberPattern      = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*\.\s*([A-Za-z_]\w*)?$`)
	wordPattern        = regexp.MustCompile(`([A-Za-z_]\w*)$`)
	callablePattern    = regexp.MustCompile(`([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*$`)
	hookPattern        = regexp.MustCompile(`(?m)^\s*(?:async\s+)?def\s+(onRequest|onResponse)\s*\(([^)]*)\)`)
	annotationPattern  = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*:\s*(Context|Request|Response|RequestSnapshot|Headers|FrozenHeaders|Queries|FrozenQueries|Body|BodySnapshot|HeaderField|QueryField|FileDescriptor|FileSnapshot|URLEncodedField|URLEncodedFieldSnapshot|MultipartPart|MultipartPartSnapshot)\b`)
	assignmentPattern  = regexp.MustCompile(`(?m)^\s*([A-Za-z_]\w*)\s*=\s*([A-Za-z_]\w*(?:\s*\.\s*[A-Za-z_]\w*)*)\s*(?:#.*)?$`)
	parameterPattern   = regexp.MustCompile(`^([A-Za-z_]\w*)`)
	whitespacePattern  = regexp.MustCompile(`\s+`)
	classReceiverTypes = map[string]string{"FileDescriptor": "FileDescriptorClass"}
)

type bindings struct {
	names []string
	types map[string]string
}

func (b *bindings) set(name, kind string) {
	if _, ok := b.types[name]; !ok {
		b.names = append(b.names, name)
	}
	b.types[name] = kind
}

func collectBindings(source string) *bindings {
	result := &bindings{types: map[string]string{}}
	result.set("context", "Context")
	result.set("request", "Request")
	result.set("response", "Response")
	for _, match := range hookPattern.FindAllStringSubmatch(source, -1) {
		var parameters []string
		for part := range strings.SplitSeq(match[2], ",") {
			if name := parameterName(part); name != "" {
				parameters = append(parameters, name)
			}
		}
		if len(parameters) > 0 {
			result.set(parameters[0], "Context")
		}
		if len(parameters) > 1 {
			kind := "Response"
			if match[1] == "onRequest" {
				kind = "Request"
			}
			result.set(parameters[1], kind)
		}
	}
	for _, match := range annotationPattern.FindAllStringSubmatch(source, -1) {
		result.set(match[1], match[2])
	}
	assignments := assignmentPattern.FindAllStringSubmatch(source, -1)
	for range 3 {
		for _, match := range assignments {
			if kind := resolveExpressionType(strip(match[2]), result); kind != "" {
				result.set(match[1], kind)
			}
		}
	}
	return result
}

func strip(value string) string { return whitespacePattern.ReplaceAllString(value, "") }

func parameterName(value string) string {
	match := parameterPattern.FindStringSubmatch(strings.TrimLeft(strings.TrimSpace(value), "*"))
	if match == nil {
		return ""
	}
	return match[1]
}

func findMember(owner, name string, methodOnly bool) *Member {
	members := load().catalog.Members[owner]
	for index := range members {
		if members[index].Name == name && (!methodOnly || members[index].Kind == "method") {
			return &members[index]
		}
	}
	return nil
}

func resolveExpressionType(expression string, known *bindings) string {
	parts := strings.Split(expression, ".")
	kind, ok := known.types[parts[0]]
	if !ok {
		kind = classReceiverTypes[parts[0]]
	}
	for _, name := range parts[1:] {
		if kind == "" {
			return ""
		}
		member := findMember(kind, name, false)
		if member == nil {
			return ""
		}
		kind = member.ResultType
	}
	return kind
}

func clampOffset(source string, offset int) int { return max(0, min(offset, len(source))) }

func hasPrefixFold(value, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix))
}

// Completions returns what to offer at offset.
func Completions(source string, offset int) CompletionResult {
	safe := clampOffset(source, offset)
	lineStart := strings.LastIndexByte(source[:safe], '\n') + 1
	prefix := source[lineStart:safe]
	if match := memberPattern.FindStringSubmatch(prefix); match != nil {
		partial := match[2]
		owner := resolveExpressionType(strip(match[1]), collectBindings(source))
		var items []Completion
		if owner != "" {
			for _, member := range load().catalog.Members[owner] {
				if hasPrefixFold(member.Name, partial) {
					items = append(items, Completion{Name: member.Name, Kind: member.Kind, Detail: owner + "." + member.Signature, InsertText: member.InsertText, Documentation: member.Documentation, Snippet: member.Kind == "method"})
				}
			}
		}
		return CompletionResult{ReplaceLength: len(partial), Items: items}
	}
	partial := ""
	if match := wordPattern.FindStringSubmatch(prefix); match != nil {
		partial = match[1]
	}
	var items []Completion
	for _, item := range topLevelCompletions(source) {
		if hasPrefixFold(item.Name, partial) {
			items = append(items, item)
		}
	}
	return CompletionResult{ReplaceLength: len(partial), Items: items}
}

func topLevelCompletions(source string) []Completion {
	known := collectBindings(source)
	var items []Completion
	for _, name := range known.names {
		kind := known.types[name]
		items = append(items, Completion{Name: name, Kind: "variable", Detail: name + ": " + kind, InsertText: name, Documentation: "FlowLens " + kind + " API object."})
	}
	for _, constructor := range load().catalog.Constructors {
		items = append(items, Completion{Name: constructor.Name, Kind: "class", Detail: constructor.Signature, InsertText: constructor.Name, Documentation: constructor.Documentation})
	}
	for _, hook := range load().catalog.Hooks {
		items = append(items, Completion{Name: hook.Name, Kind: "snippet", Detail: "def " + hook.Signature, InsertText: hook.InsertText, Documentation: hook.Documentation, Snippet: true})
	}
	return items
}

// SignatureAt returns the call the offset is inside.
func SignatureAt(source string, offset int) *Signature {
	prefix := source[:clampOffset(source, offset)]
	open := findActiveCallOpenParen(prefix)
	if open < 0 {
		return nil
	}
	match := callablePattern.FindStringSubmatch(prefix[:open])
	if match == nil {
		return nil
	}
	parts := strings.Split(strip(match[1]), ".")
	result := &Signature{}
	if len(parts) == 1 {
		found := false
		for _, constructor := range load().catalog.Constructors {
			if constructor.Name == parts[0] {
				result.Label, result.Documentation, result.Parameters, found = constructor.Signature, constructor.Documentation, constructor.Parameters, true
				break
			}
		}
		if !found {
			return nil
		}
	} else {
		owner := resolveExpressionType(strings.Join(parts[:len(parts)-1], "."), collectBindings(source))
		if owner == "" {
			return nil
		}
		member := findMember(owner, parts[len(parts)-1], true)
		if member == nil {
			return nil
		}
		result.Label, result.Documentation, result.Parameters = owner+"."+member.Signature, member.Documentation, member.Parameters
	}
	if count := len(result.Parameters); count > 0 {
		result.ActiveParameter = min(countTopLevelCommas(prefix[open+1:]), count-1)
	}
	return result
}

func isIdentifier(value byte) bool {
	return value == '_' || (value >= '0' && value <= '9') || (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f' || value == '\v'
}

// HoverAt describes the name at offset.
func HoverAt(source string, offset int) *Hover {
	if source == "" {
		return nil
	}
	cursor := min(clampOffset(source, offset), len(source)-1)
	if !isIdentifier(source[cursor]) && cursor > 0 && isIdentifier(source[cursor-1]) {
		cursor--
	}
	if !isIdentifier(source[cursor]) {
		return nil
	}
	wordStart := cursor
	for wordStart > 0 && isIdentifier(source[wordStart-1]) {
		wordStart--
	}
	expressionStart := wordStart
	for expressionStart > 0 {
		dot := expressionStart - 1
		for dot >= 0 && isSpace(source[dot]) {
			dot--
		}
		if dot < 0 || source[dot] != '.' {
			break
		}
		previousEnd := dot - 1
		for previousEnd >= 0 && isSpace(source[previousEnd]) {
			previousEnd--
		}
		if previousEnd < 0 || !isIdentifier(source[previousEnd]) {
			break
		}
		previousStart := previousEnd
		for previousStart > 0 && isIdentifier(source[previousStart-1]) {
			previousStart--
		}
		expressionStart = previousStart
	}
	wordEnd := cursor + 1
	for wordEnd < len(source) && isIdentifier(source[wordEnd]) {
		wordEnd++
	}
	parts := strings.Split(strip(source[expressionStart:wordEnd]), ".")
	known := collectBindings(source)
	if len(parts) == 1 {
		if kind, ok := known.types[parts[0]]; ok {
			return &Hover{Signature: parts[0] + ": " + kind, Documentation: "FlowLens " + kind + " API object."}
		}
		for _, constructor := range load().catalog.Constructors {
			if constructor.Name == parts[0] {
				return &Hover{Signature: constructor.Signature, Documentation: constructor.Documentation}
			}
		}
		return nil
	}
	owner := resolveExpressionType(strings.Join(parts[:len(parts)-1], "."), known)
	if owner == "" {
		return nil
	}
	member := findMember(owner, parts[len(parts)-1], false)
	if member == nil {
		return nil
	}
	label := "(property)"
	if member.Kind == "method" {
		label = "(method)"
	}
	return &Hover{Signature: label + " " + owner + "." + member.Signature, Documentation: member.Documentation}
}

type bracket struct {
	character byte
	index     int
}

func findActiveCallOpenParen(source string) int {
	var stack []bracket
	scanPython(source, func(character byte, index int) {
		switch character {
		case '(', '[', '{':
			stack = append(stack, bracket{character, index})
			return
		}
		expected := byte(0)
		switch character {
		case ')':
			expected = '('
		case ']':
			expected = '['
		case '}':
			expected = '{'
		}
		if expected == 0 {
			return
		}
		for position, s := range slices.Backward(stack) {
			if s.character == expected {
				stack = stack[:position]
				return
			}
		}
	})
	for _, s := range slices.Backward(stack) {
		if s.character == '(' {
			return s.index
		}
	}
	return -1
}

func countTopLevelCommas(value string) int {
	depth, count := 0, 0
	scanPython(value, func(character byte, _ int) {
		switch character {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth = max(0, depth-1)
		case ',':
			if depth == 0 {
				count++
			}
		}
	})
	return count
}

func scanPython(source string, visit func(character byte, index int)) {
	var quote byte
	triple, escaped, comment := false, false, false
	isTriple := func(index int, character byte) bool {
		return index+2 < len(source) && source[index+1] == character && source[index+2] == character
	}
	for index := 0; index < len(source); index++ {
		character := source[index]
		switch {
		case comment:
			if character == '\n' {
				comment = false
			}
		case quote != 0:
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case triple && character == quote && isTriple(index, quote):
				quote, triple = 0, false
				index += 2
			case !triple && character == quote:
				quote = 0
			}
		case character == '#':
			comment = true
		case character == '"' || character == '\'':
			quote = character
			triple = isTriple(index, character)
			if triple {
				index += 2
			}
		default:
			visit(character, index)
		}
	}
}
