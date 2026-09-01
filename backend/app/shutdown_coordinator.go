package app

import (
	"sync"
	"sync/atomic"
	"time"
)

type gracefulShutdownCoordinatorConfig struct {
	uiReadyTimeout time.Duration
	minimumVisible time.Duration
	showLoading    func()
	prepare        func() error
	onPrepareError func(error)
	quit           func()
}

type gracefulShutdownCoordinator struct {
	config gracefulShutdownCoordinatorConfig

	requestOnce sync.Once
	startOnce   sync.Once
	prepareOnce sync.Once

	requested   atomic.Bool
	readyToQuit atomic.Bool

	prepareDone chan struct{}
	prepareErr  error
}

func newGracefulShutdownCoordinator(config gracefulShutdownCoordinatorConfig) *gracefulShutdownCoordinator {
	return &gracefulShutdownCoordinator{
		config:      config,
		prepareDone: make(chan struct{}),
	}
}

// Request begins the user-visible shutdown sequence. It returns true only to
// the caller that won the first request, so repeated tray, signal, or menu
// requests cannot create duplicate loading or cleanup work.
func (c *gracefulShutdownCoordinator) Request() bool {
	if c == nil {
		return false
	}
	won := false
	c.requestOnce.Do(func() {
		won = true
		c.requested.Store(true)
		if c.config.showLoading != nil {
			c.config.showLoading()
		}
		if c.config.uiReadyTimeout > 0 {
			time.AfterFunc(c.config.uiReadyTimeout, func() {
				c.startPreparation(false)
			})
		}
	})
	return won
}

// UIReady acknowledges that the loading modal has been rendered and starts
// preparation without blocking the Wails event or UI thread.
func (c *gracefulShutdownCoordinator) UIReady() bool {
	if c == nil || !c.requested.Load() {
		return false
	}
	return c.startPreparation(true)
}

func (c *gracefulShutdownCoordinator) startPreparation(uiVisible bool) bool {
	won := false
	c.startOnce.Do(func() {
		won = true
		go func() {
			visibleAt := time.Now()
			err := c.PrepareAndWait()
			if uiVisible {
				remaining := c.config.minimumVisible - time.Since(visibleAt)
				if remaining > 0 {
					time.Sleep(remaining)
				}
			}
			if err != nil && c.config.onPrepareError != nil {
				c.config.onPrepareError(err)
			}
			c.readyToQuit.Store(true)
			if c.config.quit != nil {
				c.config.quit()
			}
		}()
	})
	return won
}

// PrepareAndWait runs application cleanup synchronously at most once. Wails'
// OnShutdown hook uses this as a fallback and joins preparation already started
// by the loading handshake without recursively calling Quit.
func (c *gracefulShutdownCoordinator) PrepareAndWait() error {
	if c == nil {
		return nil
	}
	c.prepareOnce.Do(func() {
		if c.config.prepare != nil {
			c.prepareErr = c.config.prepare()
		}
		close(c.prepareDone)
	})
	<-c.prepareDone
	return c.prepareErr
}

func (c *gracefulShutdownCoordinator) CanQuit() bool {
	return c != nil && c.readyToQuit.Load()
}

func (c *gracefulShutdownCoordinator) InProgress() bool {
	return c != nil && c.requested.Load() && !c.readyToQuit.Load()
}
