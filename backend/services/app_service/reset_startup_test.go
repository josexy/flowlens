package appservice

import "testing"

func TestResetStartupRetryOnlyAfterFailure(t *testing.T) {
	state := ResetStartupState{Busy: true}
	var actions []string
	service := NewResetStartupService(func() (ResetStartupState, error) { return state, nil }, func(action string) error {
		actions = append(actions, action)
		return nil
	})
	if err := service.Retry(); err == nil {
		t.Fatal("busy startup allowed retry")
	}
	state.Busy = false
	if err := service.Retry(); err == nil {
		t.Fatal("successful startup allowed retry")
	}
	state.Error = "locked file"
	if err := service.Retry(); err != nil || len(actions) != 1 || actions[0] != "retry" {
		t.Fatalf("failed startup could not retry: %v %v", actions, err)
	}
	state.Close = true
	if err := service.Retry(); err == nil {
		t.Fatal("closing startup allowed retry")
	}
}
