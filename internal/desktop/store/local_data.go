package store

import (
	"context"
	"fmt"

	"github.com/josexy/flowlens/internal/desktop/runtime"
	proxyservice "github.com/josexy/flowlens/internal/services/proxy_service"
)

const jobLocalDataClear = "local-data:clear"

// StorageBackend removes local data.
type StorageBackend interface {
	ClearCacheFiles() (proxyservice.LocalDataClearResult, error)
	ClearCacheAndHistory() (proxyservice.LocalDataClearResult, error)
}

// ClearLocalData removes the local data of scope. Once the service returns,
// on the main thread, it refreshes what the windows hold of the removed data
// and then passes the service's error to done. A partial failure refreshes
// what was removed before done reports the error. The result applies even if
// the window that asked for it has closed.
func (s *Store) ClearLocalData(scope proxyservice.LocalDataClearScope, done func(error)) {
	storage := s.storage
	runtime.Run(s.Runtime.Jobs, jobLocalDataClear, func(context.Context) (proxyservice.LocalDataClearResult, error) {
		switch {
		case storage == nil:
			return proxyservice.LocalDataClearResult{}, fmt.Errorf("local data cleanup is unavailable")
		case scope == proxyservice.LocalDataScopeCache:
			return storage.ClearCacheFiles()
		case scope == proxyservice.LocalDataScopeCacheAndHistory:
			return storage.ClearCacheAndHistory()
		default:
			return proxyservice.LocalDataClearResult{}, fmt.Errorf("unknown local data scope %d", scope)
		}
	}, func(result proxyservice.LocalDataClearResult, err error) {
		if result.Cleared {
			s.applyLocalDataCleared(result)
		}
		if done != nil {
			done(err)
		}
	})
}

// applyLocalDataCleared drops what the interface holds of removed local data.
// The live list is reset by the traffic reset event the service publishes.
func (s *Store) applyLocalDataCleared(result proxyservice.LocalDataClearResult) {
	if s.ClearProcessIcons != nil {
		s.ClearProcessIcons()
	}
	if s.History != nil {
		if result.HistoryCleared {
			s.History.forgetAll()
			if s.Workbench != nil {
				s.Workbench.LeaveHistory()
			}
		} else {
			s.History.LoadList()
		}
	}
	if s.Workspace != nil {
		s.Workspace.ClearRequestDraftCacheFileReferences(result.RequestDraftCacheRoot)
	}
}
