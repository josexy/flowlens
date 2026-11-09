package rewriteservice

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/flowlens/backend/pkg/database"
)

func testService(t *testing.T) *RewriteService {
	t.Helper()
	db, err := database.OpenAt(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db)
	if err = s.Load(); err != nil {
		t.Fatal(err)
	}
	return s
}
func mustGetState(t *testing.T, s *RewriteService) State {
	t.Helper()
	state, err := s.GetState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func testRule() Rule {
	return Rule{Name: "test", Method: "ALL", URLPattern: "https://api.example.com/users/*?token=*", Action: Action{Version: 1, Type: ActionRedirect, TargetURL: "https://test.example.com/accounts/${1}?token=${2}", HostPolicy: "target", Body: BodyAction{Mode: "none"}}}
}
func TestPatternAndTarget(t *testing.T) {
	s := testService(t)
	rule := testRule()
	for _, tt := range []struct {
		url, method, target string
		match               bool
	}{
		{"HTTPS://API.EXAMPLE.COM/users/a%2Fb?token=x%2By", "POST", "https://test.example.com/accounts/a%2Fb?token=x%2By", true},
		{"https://api.example.com/Users/a?token=x", "GET", "", false},
		{"https://api.example.com/users/a?Token=x", "GET", "", false},
		{"https://api.example.com/users/a?token=x", "CONNECT", "", false},
	} {
		p, err := s.Preview(rule, tt.method, tt.url)
		if err != nil || p.Matched != tt.match || p.TargetURL != tt.target {
			t.Fatalf("%+v: %+v %v", tt, p, err)
		}
	}
	rule.URLPattern = `https://api.example.com/\*?q=*`
	rule.Action.TargetURL = "https://test.example.com/$${literal}/${1}"
	p, err := s.Preview(rule, "GET", "https://api.example.com/*?q=Ab%20Cd")
	if err != nil || !p.Matched || p.TargetURL != "https://test.example.com/${literal}/Ab%20Cd" {
		t.Fatalf("escaped pattern %+v %v", p, err)
	}
	rule.Method = "POST"
	p, err = s.Preview(rule, "GET", "https://api.example.com/*?q=x")
	if err != nil || p.Matched {
		t.Fatal(p, err)
	}
}

func TestMetadataMutationsPreserveCompiledRulesAndStoredActions(t *testing.T) {
	s := testService(t)
	rule := testRule()
	rule.Action = Action{Version: ConfigVersion, Type: ActionResponse, Body: BodyAction{Mode: "regex", Pattern: "a+", Replacement: strings.Repeat("b", 1024*1024)}}
	st, err := s.SaveRule(rule, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.SaveRule(testRule(), st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	id, otherID := st.Rules[0].ID, st.Rules[1].ID
	before := s.Snapshot()
	// Metadata-only writes must not even name action_json in an UPDATE.
	_, err = s.db.Exec(`CREATE TRIGGER reject_action_update BEFORE UPDATE OF action_json ON rewrite_rules BEGIN SELECT RAISE(ABORT, 'unexpected action rewrite'); END`)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.SetEnabled(true, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.SetRuleEnabled(id, true, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.ReorderRules([]string{otherID, id}, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	after := s.Snapshot()
	if after.rules[1].body != before.rules[0].body || after.rules[1].matcher != before.rules[0].matcher {
		t.Fatal("metadata mutation recompiled unchanged rule")
	}
	if before.rules[0].rule.Enabled || !after.rules[1].rule.Enabled {
		t.Fatal("mutation changed a previously published snapshot")
	}
	if _, err = s.db.Exec(`DROP TRIGGER reject_action_update`); err != nil {
		t.Fatal(err)
	}
	// Editing one rule must leave all other action rows untouched as well.
	_, err = s.db.Exec(`CREATE TRIGGER reject_other_action_update BEFORE UPDATE OF action_json ON rewrite_rules WHEN OLD.id != '` + otherID + `' BEGIN SELECT RAISE(ABORT, 'unrelated action rewrite'); END`)
	if err != nil {
		t.Fatal(err)
	}
	edited := st.Rules[0]
	edited.Action.TargetURL = "https://changed.example/${1}"
	if _, err = s.SaveRule(edited, st.Revision); err != nil {
		t.Fatal(err)
	}
	s = New(s.db)
	if err = s.Load(); err != nil {
		t.Fatal(err)
	}
	loaded := mustGetState(t, s)
	if !loaded.Enabled || !loaded.Rules[1].Enabled || loaded.Rules[0].Action.TargetURL != edited.Action.TargetURL {
		t.Fatal("incremental writes did not survive reload")
	}
}
func TestSnapshotsRevisionPersistenceAndRollback(t *testing.T) {
	s := testService(t)
	st, err := s.SaveRule(testRule(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if st.Rules[0].Enabled {
		t.Fatal("new rules must be disabled")
	}
	st, err = s.SetRuleEnabled(st.Rules[0].ID, true, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.SetEnabled(true, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	old := s.Snapshot()
	matches := old.Match("GET", "https://api.example.com/users/a?token=b")
	if len(matches) != 1 {
		t.Fatal(matches)
	}
	rule := st.Rules[0]
	rule.Action.TargetURL = "https://next.example.com/${1}"
	st, err = s.SaveRule(rule, st.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if target, _ := matches[0].TargetURL(); target != "https://test.example.com/accounts/a?token=b" {
		t.Fatal(target)
	}
	if _, err = s.SetEnabled(false, st.Revision-1); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal(err)
	}
	reloaded := New(s.db)
	if err = reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if mustGetState(t, reloaded).Revision != st.Revision {
		t.Fatal("persistence")
	}
	_, err = s.db.Exec(`CREATE TRIGGER fail_rewrite BEFORE UPDATE ON rewrite_rules BEGIN SELECT RAISE(ABORT,'test'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetRuleEnabled(rule.ID, false, st.Revision); err == nil {
		t.Fatal("expected DB failure")
	}
	if mustGetState(t, s).Revision != st.Revision || !mustGetState(t, s).Rules[0].Enabled {
		t.Fatal("failed save published")
	}
	reloaded = New(s.db)
	if err = reloaded.Load(); err != nil {
		t.Fatal(err)
	}
	if mustGetState(t, reloaded).Revision != st.Revision {
		t.Fatal("failed save committed revision")
	}
}
func TestConcurrentWritersAndDetachedRules(t *testing.T) {
	s := testService(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for range 8 {
		wg.Go(func() {
			_, err := s.SaveRule(testRule(), 0)
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(err, ErrRevisionConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if success != 1 {
		t.Fatal(success)
	}
	state := mustGetState(t, s)
	state.Rules[0].Name = "mutated"
	if mustGetState(t, s).Rules[0].Name == "mutated" {
		t.Fatal("mutable snapshot exposed")
	}
	if _, err := s.ReorderRules([]string{"missing"}, 1); err == nil {
		t.Fatal("invalid order accepted")
	}
}
func TestUnknownVersionPreserved(t *testing.T) {
	s := testService(t)
	raw := `{"version":99,"future":{"keep":true}}`
	_, err := s.db.Exec(`INSERT INTO rewrite_rules VALUES('future','future',1,0,'ALL','*',?,1,1)`, raw)
	if err != nil {
		t.Fatal(err)
	}
	s = New(s.db)
	if err = s.Load(); err != nil {
		t.Fatal(err)
	}
	if mustGetState(t, s).Rules[0].UnavailableReason == "" {
		t.Fatal("unknown config available")
	}
	if _, err = s.SetEnabled(true, 0); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshot().Match("GET", "https://example.com")) != 0 {
		t.Fatal("unknown config executed")
	}
	var got string
	if err = s.db.QueryRow(`SELECT action_json FROM rewrite_rules WHERE id='future'`).Scan(&got); err != nil || got != raw {
		t.Fatal(got, err)
	}
}

func TestRemovedActionsRejectedAndPersistedRulesUnavailable(t *testing.T) {
	for _, action := range []string{"dropRequest", "dropResponse"} {
		t.Run(action, func(t *testing.T) {
			s := testService(t)
			rule := testRule()
			rule.Action.Type = action
			if _, err := s.SaveRule(rule, 0); err == nil || !strings.Contains(err.Error(), "unsupported rewrite action") {
				t.Fatalf("removed action saved: %v", err)
			}
			if _, err := s.Preview(rule, "GET", "https://api.example.com/users/a?token=b"); err == nil {
				t.Fatal("removed action previewed")
			}
			if state := mustGetState(t, s); state.Revision != 0 || len(state.Rules) != 0 {
				t.Fatalf("rejected action changed state: %+v", state)
			}

			raw := `{"version":1,"type":"` + action + `","body":{"mode":"none"}}`
			if _, err := s.db.Exec(`INSERT INTO rewrite_rules VALUES('removed','removed',1,0,'ALL','*',?,1,1)`, raw); err != nil {
				t.Fatal(err)
			}
			s = New(s.db)
			if err := s.Load(); err != nil {
				t.Fatal(err)
			}
			if got := mustGetState(t, s).Rules[0].UnavailableReason; got != "unsupported rewrite action" {
				t.Fatalf("removed action available: %q", got)
			}
			if _, err := s.SetRuleEnabled("removed", true, 0); err == nil {
				t.Fatal("removed action enabled")
			}
			state, err := s.SetEnabled(true, 0)
			if err != nil {
				t.Fatal(err)
			}
			state, err = s.SaveRule(testRule(), state.Revision)
			if err != nil {
				t.Fatal(err)
			}
			state, err = s.SetRuleEnabled(state.Rules[1].ID, true, state.Revision)
			if err != nil {
				t.Fatal(err)
			}
			matches := s.Snapshot().Match("GET", "https://api.example.com/users/a?token=b")
			if len(matches) != 1 || matches[0].Rule.Action.Type != ActionRedirect {
				t.Fatalf("unsupported action executed or valid rule lost: %+v", matches)
			}
			var persisted string
			if err := s.db.QueryRow(`SELECT action_json FROM rewrite_rules WHERE id='removed'`).Scan(&persisted); err != nil || persisted != raw {
				t.Fatalf("unsupported configuration changed: %q, %v", persisted, err)
			}
			if _, err := s.DeleteRule("removed", state.Revision); err != nil {
				t.Fatal(err)
			}
			s = New(s.db)
			if err := s.Load(); err != nil {
				t.Fatal(err)
			}
			if rules := mustGetState(t, s).Rules; len(rules) != 1 || rules[0].Action.Type != ActionRedirect {
				t.Fatalf("unsupported rule deletion failed: %+v", rules)
			}
		})
	}
}
func TestHeaderValidationAndBodyReplacement(t *testing.T) {
	s := testService(t)
	rule := testRule()
	rule.Action.Type = ActionRequest
	rule.Action.Headers = []FieldOperation{{"add", "Content-Length", "4"}}
	if _, err := s.SaveRule(rule, 0); err == nil {
		t.Fatal("managed header accepted")
	}
	rule.Action.Headers = nil
	rule.Action.Body = BodyAction{Mode: "regex", Pattern: `(foo)(\d+)`, Replacement: `${2}:$1:$$`}
	c, err := compileRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	m := Match{Rule: rule, body: c.body}
	got, changed, err := m.ReplaceBody(context.Background(), "foo42 foo99")
	if err != nil || !changed || got != "42:foo:$ 99:foo:$" {
		t.Fatal(got, changed, err)
	}
	got, changed, err = m.ReplaceBody(context.Background(), "bar")
	if err != nil || changed || got != "bar" {
		t.Fatal(got, changed, err)
	}
	m.Rule.Action.Body.Replacement = strings.Repeat("a", MaxBodyBytes)
	if _, _, err = m.ReplaceBody(context.Background(), "foo1foo2"); err == nil {
		t.Fatal("oversized expansion accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = m.ReplaceBody(ctx, "foo1"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestActualURLComponentCaseAndDynamicTargets(t *testing.T) {
	s := testService(t)
	r := testRule()
	r.URLPattern = "https://*FOO*"
	r.Action.Type = ActionRequest
	p, err := s.Preview(r, "GET", "https://example.com/foo")
	if err != nil || p.Matched {
		t.Fatal(p, err)
	}
	r.URLPattern = "*EXAMPLE.com/Path"
	p, err = s.Preview(r, "GET", "https://example.COM/Path")
	if err != nil || !p.Matched {
		t.Fatal(p, err)
	}
	r = testRule()
	r.URLPattern = "*://example.com:*/*"
	r.Action.TargetURL = "${1}://next.example.com:${2}/${3}"
	p, err = s.Preview(r, "GET", "https://example.com:8443/Path")
	if err != nil || p.TargetURL != "https://next.example.com:8443/Path" {
		t.Fatal(p, err)
	}
}
func TestReplacementMatchesGoSyntax(t *testing.T) {
	r := testRule()
	r.Action.Type = ActionRequest
	for _, pattern := range []string{`(?P<word>foo)(\d*)`, `\Boo`, ``, `.`, `(?s).*`} {
		for _, replacement := range []string{`${!$0$0}`, `${word}:$2:$$`, `${}`, `$`, `${1x}`, `${!$1}$1`} {
			r.Action.Body = BodyAction{Mode: "regex", Pattern: pattern, Replacement: replacement}
			c, err := compileRule(r)
			if err != nil {
				t.Fatal(err)
			}
			input := "foo42 foo"
			got, _, err := (Match{Rule: r, body: c.body}).ReplaceBody(context.Background(), input)
			want := c.body.ReplaceAllString(input, replacement)
			if err != nil || got != want {
				t.Fatalf("%q / %q = %q want %q: %v", pattern, replacement, got, want, err)
			}
		}
	}
}

func TestTargetTemplateRejectsInvalidStaticStructure(t *testing.T) {
	s := testService(t)
	r := testRule()
	for _, target := range []string{"relative/${1}", "https://user:pass@example.com/${1}", "https://example.com:99999/${1}", "https://example.com/${1}#fragment", "relative/$${1}"} {
		r.Action.TargetURL = target
		if _, err := s.SaveRule(r, 0); err == nil {
			t.Fatalf("saved invalid target %q", target)
		}
	}
	r.Action.TargetURL = "HTTPS://example.com/${1}"
	if _, err := s.SaveRule(r, 0); err != nil {
		t.Fatal(err)
	}
}

func TestTargetTemplateDynamicComponents(t *testing.T) {
	s := testService(t)
	r := testRule()
	r.URLPattern = "https://example.com/*"
	for _, tt := range []struct{ target, capture, want string }{{"https://next.example.com:80${1}/", "80", "https://next.example.com:8080/"}, {"https://[${1}]/", "::1", "https://[::1]/"}} {
		r.Action.TargetURL = tt.target
		p, err := s.Preview(r, "GET", "https://example.com/"+tt.capture)
		if err != nil || p.TargetURL != tt.want {
			t.Fatal(p, err)
		}
	}
}

func TestBodyScanChecksExpiredDeadline(t *testing.T) {
	r := testRule()
	r.Action.Type = ActionRequest
	r.Action.Body = BodyAction{Mode: "regex", Pattern: "a.*b"}
	c, err := compileRule(r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, _, err = (Match{Rule: r, body: c.body}).ReplaceBody(ctx, strings.Repeat("a", MaxBodyBytes))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
