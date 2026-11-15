// Package parity checks that the native interface keeps what the FlowLens
// 1.0.5 inventory in docs/technical/ui-parity.md lists.
package parity

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

const root = "../../../.."

// inventoryKeys returns the 1.0.5 locale keys in the tooltip table.
func inventoryKeys(t *testing.T) []string {
	t.Helper()
	doc, err := os.ReadFile(filepath.Join(root, "docs/technical/ui-parity.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, table, ok := strings.Cut(string(doc), "## Tooltips")
	if !ok {
		t.Fatal("no Tooltips section")
	}
	if next := strings.Index(table, "\n## "); next >= 0 {
		table = table[:next]
	}
	key := regexp.MustCompile("`([a-z_]+(?:\\.[A-Za-z_]+)+)`")
	var keys []string
	for line := range strings.SplitSeq(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 4 || !strings.HasPrefix(strings.TrimSpace(cells[1]), "`") {
			continue
		}
		for _, match := range key.FindAllStringSubmatch(cells[3], -1) {
			if !slices.Contains(keys, match[1]) {
				keys = append(keys, match[1])
			}
		}
	}
	if len(keys) < 100 {
		t.Fatalf("read only %d keys from the inventory", len(keys))
	}
	return keys
}

// catalogKey converts a 1.0.5 key, such as detail.sse.copy_message, to the
// catalog's key, detailSseCopyMessage.
func catalogKey(key string) string {
	var out strings.Builder
	upper := false
	for index, r := range key {
		switch {
		case r == '.' || r == '_':
			upper = true
		case upper && index > 0:
			out.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func accessor(key string) string {
	name := catalogKey(key)
	return strings.ToUpper(name[:1]) + name[1:]
}

// tooltipAccessors returns the locale accessors referenced from code that
// builds a tooltip. A function builds one when it calls nui.Tooltip or
// Element.Tooltip, or calls a function that does. Accessors count when they
// appear in such a function; in a function returning a string that it calls,
// such as a label helper; or in a package-level table that it reads, such as
// a list of sidebar items. Functions are matched by name within a package, so
// the check is generous rather than exact.
func tooltipAccessors(t *testing.T) map[string]bool {
	t.Helper()
	type function struct {
		pkg       string
		calls     map[string]bool
		idents    map[string]bool
		accessors []string
		tooltip   bool
		label     bool
	}
	var functions []*function
	byName := map[string][]*function{}
	tables := map[string][]string{}
	collect := func(body ast.Node, pkg string, fn *function) {
		ast.Inspect(body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.CallExpr:
				switch callee := node.Fun.(type) {
				case *ast.SelectorExpr:
					if base, ok := callee.X.(*ast.Ident); ok && base.Name == "nui" && callee.Sel.Name == "Tooltip" {
						fn.tooltip = true
					}
					if callee.Sel.Name == "Tooltip" {
						fn.tooltip = true
					}
					if base, ok := callee.X.(*ast.Ident); ok && base.Name != "locales" && base.Name != "nui" && base.Name != "ui" {
						fn.calls[base.Name+"."+callee.Sel.Name] = true
					}
					fn.calls[pkg+"."+callee.Sel.Name] = true
				case *ast.Ident:
					fn.calls[pkg+"."+callee.Name] = true
				}
			case *ast.SelectorExpr:
				if base, ok := node.X.(*ast.Ident); ok && base.Name == "locales" {
					fn.accessors = append(fn.accessors, node.Sel.Name)
				}
			case *ast.Ident:
				fn.idents[pkg+"."+node.Name] = true
			}
			return true
		})
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(filepath.Join(root, "internal", "desktop"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkg := file.Name.Name
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				fn := &function{pkg: pkg, calls: map[string]bool{}, idents: map[string]bool{}}
				if results := decl.Type.Results; results != nil && len(results.List) > 0 {
					if ident, ok := results.List[0].Type.(*ast.Ident); ok && ident.Name == "string" {
						fn.label = true
					}
				}
				collect(decl.Body, pkg, fn)
				functions = append(functions, fn)
				byName[pkg+"."+decl.Name.Name] = append(byName[pkg+"."+decl.Name.Name], fn)
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					value := spec.(*ast.ValueSpec)
					for _, name := range value.Names {
						holder := &function{calls: map[string]bool{}, idents: map[string]bool{}}
						for _, expr := range value.Values {
							collect(expr, pkg, holder)
						}
						tables[pkg+"."+name.Name] = holder.accessors
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := func(fn *function, match func(*function) bool) []*function {
		var found []*function
		for name := range fn.calls {
			for _, callee := range byName[name] {
				if match(callee) {
					found = append(found, callee)
				}
			}
		}
		return found
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range functions {
			if !fn.tooltip && len(calls(fn, func(callee *function) bool { return callee.tooltip })) > 0 {
				fn.tooltip, changed = true, true
			}
		}
	}
	accessors := map[string]bool{}
	for _, fn := range functions {
		if !fn.tooltip {
			continue
		}
		for _, name := range fn.accessors {
			accessors[name] = true
		}
		for _, helper := range calls(fn, func(callee *function) bool { return callee.label }) {
			for _, name := range helper.accessors {
				accessors[name] = true
			}
		}
		for ident := range fn.idents {
			for _, name := range tables[ident] {
				accessors[name] = true
			}
		}
	}
	return accessors
}

func TestTooltipInventoryKeysExistInCatalogs(t *testing.T) {
	for _, file := range []string{"en.json", "zh-Hans.json"} {
		data, err := os.ReadFile(filepath.Join(root, "locales", file))
		if err != nil {
			t.Fatal(err)
		}
		catalog := map[string]any{}
		if err := json.Unmarshal(data, &catalog); err != nil {
			t.Fatal(err)
		}
		for _, key := range inventoryKeys(t) {
			if _, ok := catalog[catalogKey(key)]; !ok {
				t.Errorf("%s: no %s (from %s)", file, catalogKey(key), key)
			}
		}
	}
}

func TestTooltipInventoryKeysHaveTooltipCallSites(t *testing.T) {
	accessors := tooltipAccessors(t)
	for _, key := range inventoryKeys(t) {
		if !accessors[accessor(key)] {
			t.Errorf("locales.%s (%s) is not referenced from a tooltip call site", accessor(key), key)
		}
	}
}
