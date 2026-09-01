package pythonpluginservice

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	appdatabase "github.com/josexy/flowlens/backend/pkg/database"
)

const (
	testPluginIDOne = "11111111-1111-4111-8111-111111111111"
	testPluginIDTwo = "22222222-2222-4222-8222-222222222222"
	testRuleIDOne   = "33333333-3333-4333-8333-333333333333"
	testRuleIDTwo   = "44444444-4444-4444-8444-444444444444"
)

func TestRepositoryPluginCRUDAndStableReorder(t *testing.T) {
	repository := newTestRepository(t)
	ctx := context.Background()

	first, err := repository.createPlugin(ctx, CreatePluginInput{
		ID: testPluginIDOne, Name: "First", Description: "one", ParamsJSON: `{"token":"redacted"}`,
	})
	if err != nil {
		t.Fatalf("create first plugin: %v", err)
	}
	second, err := repository.createPlugin(ctx, CreatePluginInput{
		ID: testPluginIDTwo, Name: "Second", Description: "two", ParamsJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("create second plugin: %v", err)
	}
	if first.SortOrder != 0 || second.SortOrder != 1 {
		t.Fatalf("initial sort order = %d, %d; want 0, 1", first.SortOrder, second.SortOrder)
	}

	if err := repository.reorderPlugins(ctx, []string{testPluginIDTwo, testPluginIDOne}); err != nil {
		t.Fatalf("reorder plugins: %v", err)
	}
	plugins, err := repository.listPlugins(ctx)
	if err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	if got := []string{plugins[0].ID, plugins[1].ID}; !reflect.DeepEqual(got, []string{testPluginIDTwo, testPluginIDOne}) {
		t.Fatalf("plugin order = %#v", got)
	}

	updated, err := repository.updatePlugin(ctx, testPluginIDOne, UpdatePluginInput{
		Name: "Renamed", Description: "updated", ParamsJSON: `{"count":2}`,
	})
	if err != nil {
		t.Fatalf("update plugin: %v", err)
	}
	if updated.Name != "Renamed" || updated.ParamsJSON != `{"count":2}` {
		t.Fatalf("updated plugin = %+v", updated)
	}
	if err := repository.setPluginEnabled(ctx, testPluginIDOne, true); err != nil {
		t.Fatalf("enable plugin: %v", err)
	}
	if err := repository.setPluginActivation(ctx, testPluginIDOne, "rev-a", ValidationStatusValid, ""); err != nil {
		t.Fatalf("activate plugin: %v", err)
	}
	got, err := repository.getPlugin(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("get plugin: %v", err)
	}
	if !got.Enabled || got.ActiveRevision != "rev-a" || got.LastGoodRevision != "rev-a" || got.ValidationStatus != ValidationStatusValid {
		t.Fatalf("activated plugin = %+v", got)
	}

	if err := repository.deletePlugin(ctx, testPluginIDTwo); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	plugins, err = repository.listPlugins(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(plugins) != 1 || plugins[0].ID != testPluginIDOne || plugins[0].SortOrder != 0 {
		t.Fatalf("plugins after delete = %+v", plugins)
	}
}

func TestRepositoryRejectsInvalidParamsWithoutMutation(t *testing.T) {
	repository := newTestRepository(t)
	ctx := context.Background()
	if _, err := repository.createPlugin(ctx, CreatePluginInput{
		ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
	}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	for _, invalid := range []string{"", "null", "[]", `{"broken"`, `{"nan":NaN}`} {
		if _, err := repository.updatePlugin(ctx, testPluginIDOne, UpdatePluginInput{
			Name: "Changed", ParamsJSON: invalid,
		}); err == nil {
			t.Fatalf("invalid params %q were accepted", invalid)
		}
	}
	plugin, err := repository.getPlugin(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("get plugin: %v", err)
	}
	if plugin.Name != "Plugin" || plugin.ParamsJSON != `{}` {
		t.Fatalf("invalid update mutated plugin: %+v", plugin)
	}
}

func TestRepositoryRuleCRUDValidationAndReorder(t *testing.T) {
	repository := newTestRepository(t)
	ctx := context.Background()
	if _, err := repository.createPlugin(ctx, CreatePluginInput{
		ID: testPluginIDOne, Name: "Plugin", ParamsJSON: `{}`,
	}); err != nil {
		t.Fatalf("create plugin: %v", err)
	}

	first, err := repository.createRule(ctx, testPluginIDOne, CreateRuleInput{
		ID: testRuleIDOne, Enabled: true, Method: "GET", URLPattern: "https://example.com/api/*",
	})
	if err != nil {
		t.Fatalf("create first rule: %v", err)
	}
	second, err := repository.createRule(ctx, testPluginIDOne, CreateRuleInput{
		ID: testRuleIDTwo, Enabled: true, Method: "*", URLPattern: "*",
	})
	if err != nil {
		t.Fatalf("create second rule: %v", err)
	}
	if first.SortOrder != 0 || second.SortOrder != 1 {
		t.Fatalf("initial rule order = %d, %d", first.SortOrder, second.SortOrder)
	}

	for _, input := range []CreateRuleInput{
		{Enabled: true, Method: "get", URLPattern: "*"},
		{Enabled: true, Method: "GE T", URLPattern: "*"},
		{Enabled: true, Method: "GET", URLPattern: ""},
	} {
		if _, err := repository.createRule(ctx, testPluginIDOne, input); err == nil {
			t.Fatalf("invalid rule was accepted: %+v", input)
		}
	}

	if err := repository.reorderRules(ctx, testPluginIDOne, []string{testRuleIDTwo, testRuleIDOne}); err != nil {
		t.Fatalf("reorder rules: %v", err)
	}
	rules, err := repository.listRules(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if got := []string{rules[0].ID, rules[1].ID}; !reflect.DeepEqual(got, []string{testRuleIDTwo, testRuleIDOne}) {
		t.Fatalf("rule order = %#v", got)
	}

	updated, err := repository.updateRule(ctx, testPluginIDOne, testRuleIDOne, UpdateRuleInput{
		Enabled: false, Method: "POST", URLPattern: "https://example.com/v?/*",
	})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.Enabled || updated.Method != "POST" {
		t.Fatalf("updated rule = %+v", updated)
	}
	if err := repository.deleteRule(ctx, testPluginIDOne, testRuleIDTwo); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	rules, err = repository.listRules(ctx, testPluginIDOne)
	if err != nil {
		t.Fatalf("list rules after delete: %v", err)
	}
	if len(rules) != 1 || rules[0].SortOrder != 0 {
		t.Fatalf("rules after delete = %+v", rules)
	}
}

func TestRepositoryReorderIsTransactional(t *testing.T) {
	repository := newTestRepository(t)
	ctx := context.Background()
	for _, input := range []CreatePluginInput{
		{ID: testPluginIDOne, Name: "First", ParamsJSON: `{}`},
		{ID: testPluginIDTwo, Name: "Second", ParamsJSON: `{}`},
	} {
		if _, err := repository.createPlugin(ctx, input); err != nil {
			t.Fatalf("create plugin: %v", err)
		}
	}
	if err := repository.reorderPlugins(ctx, []string{testPluginIDTwo, "missing"}); err == nil {
		t.Fatal("reorder with a missing plugin unexpectedly succeeded")
	}
	plugins, err := repository.listPlugins(ctx)
	if err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	if got := []string{plugins[0].ID, plugins[1].ID}; !reflect.DeepEqual(got, []string{testPluginIDOne, testPluginIDTwo}) {
		t.Fatalf("failed reorder mutated order: %#v", got)
	}
}

func newTestRepository(t *testing.T) *repository {
	t.Helper()
	db, err := appdatabase.OpenAt(filepath.Join(t.TempDir(), "flowlens.db"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return newRepository(db)
}
