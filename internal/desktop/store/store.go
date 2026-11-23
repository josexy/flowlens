// Package store holds the interface's application state. State changes only
// on the main thread; slow service calls run as jobs whose results return to
// the main thread through the dispatcher.
package store

import (
	"github.com/josexy/flowlens/internal/desktop/logic"
	"github.com/josexy/flowlens/internal/desktop/runtime"
	rewriteservice "github.com/josexy/flowlens/internal/services/rewrite_service"
)

// Runtime carries the interface's threading primitives into each store.
type Runtime struct {
	Dispatcher *runtime.Dispatcher
	Jobs       *runtime.Jobs
}

// NewRuntime returns a Runtime with a new dispatcher.
func NewRuntime() Runtime {
	dispatcher := runtime.NewDispatcher()
	return Runtime{Dispatcher: dispatcher, Jobs: runtime.NewJobs(dispatcher)}
}

// Close stops job delivery and waits for running jobs.
func (r Runtime) Close() {
	r.Jobs.Close()
	r.Dispatcher.Close()
}

// Topic is a service topic as the stores use it. serviceruntime.Topic
// satisfies it. A subscriber only posts to the dispatcher, so it never
// blocks the publisher.
type Topic[T any] interface {
	Subscribe(fn func(T)) (off func())
}

// Store is the root of the application state.
type Store struct {
	Runtime    Runtime
	Settings   *SettingsStore
	Workbench  *WorkbenchStore
	Events     *EventRouter
	Traffic    *TrafficStore
	Filter     *FilterStore
	Proxy      *ProxyStore
	History    *HistoryStore
	Workspace  *WorkspaceStore
	Collection *CollectionStore
	Rewrite    *RewriteStore
	Plugins    *PluginsStore
	Updater    *UpdaterStore
	MemStats   *MemStatsStore
	// ShuttingDown makes the main window show the shutdown indicator.
	ShuttingDown bool
	// ClearProcessIcons forgets the process icons the windows hold, after
	// local data is cleared.
	ClearProcessIcons func()

	storage StorageBackend
}

// Dependencies are the services the stores use.
type Dependencies struct {
	Settings     SettingsDependencies
	Storage      StorageBackend
	Updater      UpdaterBackend
	MemStats     MemStatsBackend
	Traffic      TrafficBackend
	Proxy        ProxyBackend
	History      HistoryBackend
	Requests     RequestBackend
	Saved        SavedRequestBackend
	Collection   CollectionBackend
	Rewrite      RewriteBackend
	Plugins      PluginBackend
	PluginEvents PluginEvents
	// RewriteChanges publishes rule changes.
	RewriteChanges Topic[rewriteservice.Changed]
}

// New returns a Store with empty state.
func New(rt Runtime, dependencies Dependencies) *Store {
	events := NewEventRouter()
	data := &Store{
		Runtime:   rt,
		Settings:  NewSettingsStore(rt, dependencies.Settings),
		Workbench: NewWorkbenchStore(),
		Events:    events,
		storage:   dependencies.Storage,
	}
	if dependencies.Traffic != nil {
		data.Traffic = NewTrafficStore(rt, dependencies.Traffic, events, nil)
		data.Filter = NewFilterStore(func() ([]*logic.TrafficEntry, uint64) {
			return data.Traffic.Entries, data.Traffic.EntriesVersion
		})
	}
	if dependencies.MemStats != nil {
		data.MemStats = NewMemStatsStore(rt, dependencies.MemStats)
	}
	if dependencies.Updater != nil {
		data.Updater = NewUpdaterStore(rt, dependencies.Updater)
	}
	if dependencies.Proxy != nil {
		data.Proxy = NewProxyStore(rt, dependencies.Proxy)
	}
	if dependencies.History != nil {
		data.History = NewHistoryStore(rt, dependencies.History)
	}
	if dependencies.Requests != nil {
		data.Workspace = NewWorkspaceStore(rt, dependencies.Requests, dependencies.Saved, nil)
	}
	if dependencies.Rewrite != nil {
		data.Rewrite = NewRewriteStore(rt, dependencies.Rewrite, dependencies.RewriteChanges)
	}
	if dependencies.Plugins != nil {
		data.Plugins = NewPluginsStore(rt, dependencies.Plugins, dependencies.PluginEvents)
	}
	if dependencies.Collection != nil {
		data.Collection = NewCollectionStore(rt, dependencies.Collection, data.Workspace)
	}
	return data
}
