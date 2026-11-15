// Package pythonapi describes the FlowLens Python SDK: the code assistance of
// the Python editors, the reference, and the system prompts.
//
// The data files are generated from the same catalog the Python runtime is
// documented against. Keep them aligned with it.
package pythonapi

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/catalog.json data/help.en.json data/help.zh.json
var dataFiles embed.FS

// Member is a property or method of an SDK type.
type Member struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Signature     string   `json:"signature"`
	InsertText    string   `json:"insertText"`
	Documentation string   `json:"documentation"`
	Parameters    []string `json:"parameters"`
	ResultType    string   `json:"resultType"`
	Writable      bool     `json:"writable"`
	ReturnType    string   `json:"returnType"`
}

// Constructor builds an SDK type.
type Constructor struct {
	Name          string   `json:"name"`
	Signature     string   `json:"signature"`
	Documentation string   `json:"documentation"`
	Parameters    []string `json:"parameters"`
}

// Hook is a function a script defines.
type Hook struct {
	Name          string   `json:"name"`
	Signature     string   `json:"signature"`
	Parameters    []string `json:"parameters"`
	ReturnType    string   `json:"returnType"`
	InsertText    string   `json:"insertText"`
	Documentation string   `json:"documentation"`
}

type catalog struct {
	Members       map[string][]Member `json:"members"`
	MemberOrder   []string            `json:"memberOrder"`
	Constructors  []Constructor       `json:"constructors"`
	Hooks         []Hook              `json:"hooks"`
	MinimalScript string              `json:"minimalScript"`
}

// Step is a titled instruction.
type Step struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Messages are the texts of the reference in one language.
type Messages struct {
	Title            string                       `json:"title"`
	Version          string                       `json:"version"`
	Close            string                       `json:"close"`
	Quickstart       string                       `json:"quickstart"`
	Reference        string                       `json:"reference"`
	CopyContext      string                       `json:"copy_context"`
	CopyTemplate     string                       `json:"copy_template"`
	CopiedContext    string                       `json:"copied_context"`
	CopiedTemplate   string                       `json:"copied_template"`
	Search           string                       `json:"search"`
	NoResults        string                       `json:"no_results"`
	NoResultsHint    string                       `json:"no_results_hint"`
	Readonly         string                       `json:"readonly"`
	Writable         string                       `json:"writable"`
	Method           string                       `json:"method"`
	Constructor      string                       `json:"constructor"`
	Hook             string                       `json:"hook"`
	Importable       string                       `json:"importable"`
	Provided         string                       `json:"provided"`
	ProvidedHint     string                       `json:"provided_hint"`
	MinimalTemplate  string                       `json:"minimal_template"`
	Global           string                       `json:"global"`
	Inline           string                       `json:"inline"`
	ScopeGlobal      string                       `json:"scope_global"`
	ScopeInline      string                       `json:"scope_inline"`
	UsageNotes       string                       `json:"usage_notes"`
	ImportNotes      string                       `json:"import_notes"`
	RuntimeDetails   string                       `json:"runtime_details"`
	RuntimeSummary   []string                     `json:"runtime_summary"`
	Parameters       string                       `json:"parameters"`
	Returns          string                       `json:"returns"`
	PromptIntro      string                       `json:"prompt_intro"`
	PromptRules      string                       `json:"prompt_rules"`
	PromptAPI        string                       `json:"prompt_api"`
	PromptTemplate   string                       `json:"prompt_template"`
	PromptOutput     string                       `json:"prompt_output"`
	PromptOutputText string                       `json:"prompt_output_text"`
	PromptReq        string                       `json:"prompt_requirement"`
	PromptHolder     string                       `json:"prompt_placeholder"`
	Rules            []Step                       `json:"rules"`
	Members          map[string]map[string]string `json:"members"`
	Hooks            map[string]string            `json:"hooks"`
	Constructors     map[string]string            `json:"constructors"`
	ReferenceNotes   map[string]string            `json:"reference_notes"`
	GroupSummaries   map[string]string            `json:"group_summaries"`
	Start            struct {
		AIHint          string   `json:"ai_hint"`
		Runtime         Step     `json:"runtime"`
		EssentialsTitle string   `json:"essentials_title"`
		Essentials      []string `json:"essentials"`
		Global          struct {
			Edit   Step `json:"edit"`
			Enable Step `json:"enable"`
		} `json:"global"`
		Inline struct {
			Edit   Step `json:"edit"`
			Enable Step `json:"enable"`
		} `json:"inline"`
	} `json:"start"`
}

var load = sync.OnceValue(func() *loaded {
	result := &loaded{}
	decode("data/catalog.json", &result.catalog)
	decode("data/help.en.json", &result.en)
	decode("data/help.zh.json", &result.zh)
	return result
})

type loaded struct {
	catalog catalog
	en, zh  Messages
}

func decode(name string, target any) {
	data, err := dataFiles.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("pythonapi: %v", err))
	}
	if err := json.Unmarshal(data, target); err != nil {
		panic(fmt.Sprintf("pythonapi: %s: %v", name, err))
	}
}

// MessagesFor returns the texts of a language.
func MessagesFor(locale string) *Messages {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return &load().zh
	}
	return &load().en
}

// MinimalScript returns the starter script.
func MinimalScript() string { return load().catalog.MinimalScript }

// Scopes of the reference.
const (
	ScopeGlobal = "global"
	ScopeInline = "inline"
)

// Entry is one item of the reference.
type Entry struct {
	Name, Signature, Description, Kind, ReturnType string
	Writable                                       bool
	Parameters                                     []string
}

// Group is one section of the reference.
type Group struct {
	Name, Note string
	Importable bool
	Entries    []Entry
}

func memberEntry(owner string, member Member, messages *Messages) Entry {
	displayed := owner
	if owner == "FileDescriptorClass" {
		displayed = "FileDescriptor"
	}
	description, ok := messages.Members[owner][member.Name]
	if !ok {
		description = member.Documentation
	}
	return Entry{Name: member.Name, Signature: displayed + "." + member.Signature, Description: description, Kind: member.Kind, Writable: member.Writable, Parameters: member.Parameters, ReturnType: member.ReturnType}
}

// Groups returns the reference, narrowed by query.
func Groups(locale, query string) []Group {
	data := load().catalog
	messages := MessagesFor(locale)
	hooks := Group{Name: "Hooks", Note: messages.ReferenceNotes["Hooks"]}
	for _, hook := range data.Hooks {
		hooks.Entries = append(hooks.Entries, Entry{Name: hook.Name, Signature: "def " + hook.Signature, Description: messages.Hooks[hook.Name], Kind: "hook", Parameters: hook.Parameters, ReturnType: hook.ReturnType})
	}
	groups := []Group{hooks}
	for _, owner := range data.MemberOrder {
		if owner == "FileDescriptorClass" {
			continue
		}
		group := Group{Name: owner, Note: messages.ReferenceNotes[owner], Importable: owner == "Context"}
		for _, constructor := range data.Constructors {
			if constructor.Name == owner {
				description, ok := messages.Constructors[owner]
				if !ok {
					description = constructor.Documentation
				}
				group.Importable = true
				group.Entries = append(group.Entries, Entry{Name: owner, Signature: constructor.Signature, Description: description, Kind: "constructor", Parameters: constructor.Parameters, ReturnType: owner})
			}
		}
		for _, member := range data.Members[owner] {
			group.Entries = append(group.Entries, memberEntry(owner, member, messages))
		}
		if owner == "FileDescriptor" {
			for _, member := range data.Members["FileDescriptorClass"] {
				group.Entries = append(group.Entries, memberEntry("FileDescriptorClass", member, messages))
			}
		}
		groups = append(groups, group)
	}
	search := strings.ToLower(strings.TrimSpace(query))
	if search == "" {
		return groups
	}
	var matched []Group
	for _, group := range groups {
		if strings.Contains(strings.ToLower(group.Name+"\n"+messages.GroupSummaries[group.Name]), search) {
			matched = append(matched, group)
			continue
		}
		var entries []Entry
		for _, entry := range group.Entries {
			if strings.Contains(strings.ToLower(entry.Signature+"\n"+entry.Description), search) {
				entries = append(entries, entry)
			}
		}
		if len(entries) > 0 {
			group.Entries = entries
			matched = append(matched, group)
		}
	}
	return matched
}

// SystemPrompt returns the complete prompt for a scope and language.
func SystemPrompt(scope, locale string) string {
	messages := MessagesFor(locale)
	var sections []string
	for _, group := range Groups(locale, "") {
		tag := messages.Provided
		switch {
		case group.Name == "Hooks":
			tag = messages.Hook
		case group.Importable:
			tag = messages.Importable
		}
		lines := []string{"### " + group.Name + " (" + tag + ")"}
		for _, entry := range group.Entries {
			parts := []string{"- `" + entry.Signature + "`"}
			if entry.Kind == "property" {
				access := messages.Readonly
				if entry.Writable {
					access = messages.Writable
				}
				parts = append(parts, "("+access+")")
			}
			lines = append(lines, strings.Join(append(parts, "— "+entry.Description), " "))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	scopeName, scopeText := messages.Global, messages.ScopeGlobal
	if scope == ScopeInline {
		scopeName, scopeText = messages.Inline, messages.ScopeInline
	}
	parts := []string{"# FlowLens Python SDK API v1", messages.PromptIntro, "## " + scopeName, scopeText, "## " + messages.PromptRules}
	for _, rule := range messages.Rules {
		parts = append(parts, "### "+rule.Title+"\n"+rule.Text)
	}
	return strings.Join(append(parts, messages.ProvidedHint, "## "+messages.PromptAPI, strings.Join(sections, "\n\n"),
		"## "+messages.PromptTemplate, "```python\n"+MinimalScript()+"```", "## "+messages.PromptOutput, messages.PromptOutputText,
		"## "+messages.PromptReq, messages.PromptHolder), "\n\n")
}
