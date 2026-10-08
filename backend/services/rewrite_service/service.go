package rewriteservice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/josexy/flowlens/backend/pkg/logger"
	"github.com/wailsapp/wails/v3/pkg/application"
)

var ErrRevisionConflict = errors.New("rewrite rules revision conflict; reload before saving")

type RewriteService struct {
	db       *sql.DB
	mu       sync.Mutex
	snapshot atomic.Pointer[Snapshot]
	changed  func(Changed)

	loadOnce   sync.Once
	loadDone   chan struct{}
	loadCtx    context.Context
	loadCancel context.CancelFunc
	loadErr    error // written once, before loadDone closes
}

func init() { application.RegisterEvent[Changed](ChangedEvent) }
func New(db *sql.DB) *RewriteService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RewriteService{db: db, loadDone: make(chan struct{}), loadCtx: ctx, loadCancel: cancel}
}
func SetChangedHandler(s *RewriteService, f func(Changed)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changed = f
}

//wails:ignore
func (s *RewriteService) Snapshot() *Snapshot {
	if s == nil {
		return nil
	}
	return s.snapshot.Load()
}

func (s *RewriteService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	s.startLoading(ctx)
	return nil
}

func (s *RewriteService) ServiceShutdown() error { return s.Shutdown() }

func (s *RewriteService) startLoading(parent context.Context) {
	s.loadOnce.Do(func() {
		stop := context.AfterFunc(parent, s.loadCancel)
		if parent.Err() != nil {
			s.loadCancel()
		}
		go func() {
			defer close(s.loadDone)
			defer stop()
			if err := s.load(s.loadCtx); err != nil {
				s.loadErr = fmt.Errorf("load rewrite rules: %w", err)
				if !errors.Is(err, context.Canceled) {
					logger.G().Errorf("Load rewrite rules failed: %v", err)
				}
			}
		}()
	})
}

// Load waits for the same initial load used by ServiceStartup. A service owns
// one initial snapshot load; later changes go through revisioned mutations.
//
//wails:ignore
func (s *RewriteService) Load() error {
	s.startLoading(context.Background())
	return s.WaitReady(context.Background())
}

// WaitReady keeps consumers from treating an unpublished snapshot as disabled
// rules. Canceling a caller does not cancel the shared startup load.
//
//wails:ignore
func (s *RewriteService) WaitReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.loadDone:
		return s.readinessError()
	}
}

func (s *RewriteService) readinessError() error {
	if err := s.loadCtx.Err(); err != nil {
		return err
	}
	select {
	case <-s.loadDone:
		return s.loadErr
	default:
		return errors.New("rewrite rules are still loading")
	}
}

// Shutdown also covers early application startup failures, before Wails has
// started this service. SQLite must remain open until this returns.
//
//wails:ignore
func (s *RewriteService) Shutdown() error {
	s.loadCancel()
	s.startLoading(context.Background())
	<-s.loadDone
	s.mu.Lock()
	// Wait for any committed mutation's notification before releasing the app.
	s.changed = nil
	s.mu.Unlock()
	return nil
}

func (s *RewriteService) load(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state := State{Rules: []Rule{}}
	if err := s.db.QueryRowContext(ctx, `SELECT enabled, revision FROM rewrite_state WHERE id=1`).Scan(&state.Enabled, &state.Revision); err != nil {
		return fmt.Errorf("load rewrite state: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,enabled,method,url_pattern,action_json,created_at,updated_at FROM rewrite_rules ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var compiled []compiledRule
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var r Rule
		var action string
		if err := rows.Scan(&r.ID, &r.Name, &r.Enabled, &r.Method, &r.URLPattern, &action, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return err
		}
		decodeErr := json.Unmarshal([]byte(action), &r.Action)
		if err := ctx.Err(); err != nil {
			return err
		}
		if decodeErr != nil {
			r.UnavailableReason = "invalid persisted action configuration"
		} else if c, err := compileRule(r); err != nil {
			r.UnavailableReason = err.Error()
		} else {
			compiled = append(compiled, c)
		}
		state.Rules = append(state.Rules, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.snapshot.Store(&Snapshot{state, compiled})
	return nil
}
func (s *RewriteService) GetState(ctx context.Context) (State, error) {
	if err := s.WaitReady(ctx); err != nil {
		return State{}, err
	}
	return cloneState(s.snapshot.Load().state), nil
}

// mutate publishes only after the transaction commits. The database revision is
// checked as well as the in-process snapshot, including across service instances.
func (s *RewriteService) mutate(revision int64, edit func(*State) error) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.readinessError(); err != nil {
		return State{}, err
	}
	old := s.snapshot.Load()
	if old == nil {
		return State{}, errors.New("rewrite service is not loaded")
	}
	if old.state.Revision != revision {
		return State{}, ErrRevisionConflict
	}
	candidate := cloneState(old.state)
	if err := edit(&candidate); err != nil {
		return State{}, err
	}
	oldRules := make(map[string]int, len(old.state.Rules))
	for i, rule := range old.state.Rules {
		oldRules[rule.ID] = i
	}
	oldCompiled := make(map[string]compiledRule, len(old.rules))
	for _, rule := range old.rules {
		oldCompiled[rule.rule.ID] = rule
	}
	var compiled []compiledRule
	for i := range candidate.Rules {
		r := &candidate.Rules[i]
		// Unknown versions stay untouched in SQLite when editing other rules.
		if r.UnavailableReason != "" {
			continue
		}
		c, reusable := oldCompiled[r.ID]
		if !reusable || !sameRuleContent(c.rule, *r) {
			var err error
			c, err = compileRule(*r)
			if err != nil {
				return State{}, fmt.Errorf("%s: %w", r.Name, err)
			}
		} else {
			// Matchers are immutable; only snapshot-local metadata changes for
			// enable/order operations. Never mutate the previous snapshot.
			c.rule = cloneRule(*r)
		}
		compiled = append(compiled, c)
	}
	candidate.Revision++
	tx, err := s.db.Begin()
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE rewrite_state SET enabled=?,revision=? WHERE id=1 AND revision=?`, candidate.Enabled, candidate.Revision, revision)
	if err != nil {
		return State{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return State{}, err
	}
	if n != 1 {
		return State{}, ErrRevisionConflict
	}
	keep := make(map[string]bool, len(candidate.Rules))
	for i, r := range candidate.Rules {
		keep[r.ID] = true
		oldIndex, existed := oldRules[r.ID]
		if r.UnavailableReason != "" {
			if !existed || oldIndex != i {
				if _, err = tx.Exec(`UPDATE rewrite_rules SET sort_order=? WHERE id=?`, i, r.ID); err != nil {
					return State{}, err
				}
			}
			continue
		}
		if existed && sameRuleContent(old.state.Rules[oldIndex], r) {
			previous := old.state.Rules[oldIndex]
			if oldIndex != i || previous.Enabled != r.Enabled || previous.UpdatedAt != r.UpdatedAt {
				_, err = tx.Exec(`UPDATE rewrite_rules SET enabled=?,sort_order=?,updated_at=? WHERE id=?`, r.Enabled, i, r.UpdatedAt, r.ID)
				if err != nil {
					return State{}, err
				}
			}
			continue
		}
		action, err := json.Marshal(r.Action)
		if err != nil {
			return State{}, err
		}
		_, err = tx.Exec(`INSERT INTO rewrite_rules(id,name,enabled,sort_order,method,url_pattern,action_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,enabled=excluded.enabled,sort_order=excluded.sort_order,method=excluded.method,url_pattern=excluded.url_pattern,action_json=excluded.action_json,updated_at=excluded.updated_at`, r.ID, r.Name, r.Enabled, i, r.Method, r.URLPattern, string(action), r.CreatedAt, r.UpdatedAt)
		if err != nil {
			return State{}, err
		}
	}
	for _, r := range old.state.Rules {
		if !keep[r.ID] {
			if _, err = tx.Exec(`DELETE FROM rewrite_rules WHERE id=?`, r.ID); err != nil {
				return State{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return State{}, err
	}
	s.snapshot.Store(&Snapshot{candidate, compiled})
	if s.changed != nil {
		s.changed(Changed{candidate.Revision})
	}
	return cloneState(candidate), nil
}

// Strings are immutable: comparing metadata must not serialize large body
// strings or rewrite action_json for an enable/order-only operation.
func sameRuleContent(a, b Rule) bool {
	return a.Name == b.Name && a.Method == b.Method && a.URLPattern == b.URLPattern &&
		a.UnavailableReason == b.UnavailableReason &&
		a.Action.Version == b.Action.Version && a.Action.Type == b.Action.Type &&
		a.Action.TargetURL == b.Action.TargetURL && a.Action.HostPolicy == b.Action.HostPolicy &&
		a.Action.Body == b.Action.Body && slices.Equal(a.Action.Headers, b.Action.Headers) && slices.Equal(a.Action.Query, b.Action.Query)
}
func (s *RewriteService) SaveRule(rule Rule, expectedRevision int64) (State, error) {
	return s.mutate(expectedRevision, func(state *State) error {
		rule = cloneRule(rule)
		rule.UnavailableReason = ""
		now := time.Now().UnixMicro()
		rule.UpdatedAt = now
		if rule.ID == "" {
			rule.ID = uuid.NewString()
			rule.Enabled = false
			rule.CreatedAt = now
			state.Rules = append(state.Rules, rule)
			return nil
		}
		for i, old := range state.Rules {
			if old.ID == rule.ID {
				rule.CreatedAt = old.CreatedAt
				state.Rules[i] = rule
				return nil
			}
		}
		return errors.New("rewrite rule no longer exists")
	})
}
func (s *RewriteService) DeleteRule(id string, expectedRevision int64) (State, error) {
	return s.mutate(expectedRevision, func(state *State) error {
		for i, r := range state.Rules {
			if r.ID == id {
				state.Rules = slices.Delete(state.Rules, i, i+1)
				return nil
			}
		}
		return errors.New("rewrite rule no longer exists")
	})
}
func (s *RewriteService) SetRuleEnabled(id string, enabled bool, expectedRevision int64) (State, error) {
	return s.mutate(expectedRevision, func(state *State) error {
		for i := range state.Rules {
			r := &state.Rules[i]
			if r.ID == id {
				if r.UnavailableReason != "" {
					return errors.New(r.UnavailableReason)
				}
				r.Enabled = enabled
				r.UpdatedAt = time.Now().UnixMicro()
				return nil
			}
		}
		return errors.New("rewrite rule no longer exists")
	})
}
func (s *RewriteService) SetEnabled(enabled bool, expectedRevision int64) (State, error) {
	return s.mutate(expectedRevision, func(state *State) error { state.Enabled = enabled; return nil })
}
func (s *RewriteService) ReorderRules(ids []string, expectedRevision int64) (State, error) {
	return s.mutate(expectedRevision, func(state *State) error {
		if len(ids) != len(state.Rules) {
			return errors.New("rule order must contain every rule")
		}
		byID := make(map[string]Rule, len(ids))
		for _, r := range state.Rules {
			byID[r.ID] = r
		}
		ordered := make([]Rule, 0, len(ids))
		for _, id := range ids {
			r, ok := byID[id]
			if !ok {
				return errors.New("rule order contains missing or duplicate IDs")
			}
			ordered = append(ordered, r)
			delete(byID, id)
		}
		state.Rules = ordered
		return nil
	})
}
func (s *RewriteService) Preview(rule Rule, method, rawURL string) (PreviewResult, error) {
	c, err := compileRule(rule)
	if err != nil {
		return PreviewResult{}, err
	}
	result := PreviewResult{Captures: []string{}}
	if method == "CONNECT" || (rule.Method != "ALL" && rule.Method != method) {
		return result, nil
	}
	groups := c.matcher.FindStringSubmatch(rawURL)
	if groups == nil {
		return result, nil
	}
	result.Matched = true
	result.Captures = groups[1:]
	if rule.Action.Type == ActionRedirect {
		result.TargetURL, err = (Match{rule, groups[1:], c.body}).TargetURL()
	}
	return result, err
}
