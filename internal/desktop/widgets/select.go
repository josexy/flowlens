package widgets

import nui "github.com/josexy/nuxt-ui-go/ui"

// SelectState owns one control's options and popup for one window. Prepare
// identifies the submitted data by a locale or another explicit revision;
// unchanged frames neither allocate options nor scan the existing data.
type SelectState[T comparable] struct {
	state nui.SelectState[T]
	key   string
	ready bool
}

// Prepare submits options on initialization or a data/locale change. build is
// called immediately, never retained. The key must cover every input to build.
func (s *SelectState[T]) Prepare(key string, build func() []nui.Option[T]) *nui.SelectState[T] {
	if !s.ready || s.key != key {
		s.state.SetOptions(build())
		s.key, s.ready = key, true
	}
	return &s.state
}
