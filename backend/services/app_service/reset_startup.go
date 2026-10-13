package appservice

import "errors"

// ResetStartupState belongs only to the isolated startup WebView.
type ResetStartupState struct {
	Language string   `json:"language"`
	Stage    string   `json:"stage"`
	Stages   []string `json:"stages"`
	Error    string   `json:"error"`
	Busy     bool     `json:"busy"`
	Close    bool     `json:"close"`
}

type ResetStartupService struct {
	read   func() (ResetStartupState, error)
	action func(string) error
}

func NewResetStartupService(read func() (ResetStartupState, error), action func(string) error) *ResetStartupService {
	return &ResetStartupService{read: read, action: action}
}

func (s *ResetStartupService) GetState() (ResetStartupState, error) { return s.read() }
func (s *ResetStartupService) Show() error                          { return s.action("show") }
func (s *ResetStartupService) Ready() error                         { return s.action("ready") }
func (s *ResetStartupService) Exit() error                          { return s.action("exit") }
func (s *ResetStartupService) Retry() error {
	state, err := s.read()
	if err != nil {
		return err
	}
	if state.Busy || state.Error == "" || state.Close {
		return errors.New("application_reset_in_progress")
	}
	return s.action("retry")
}
