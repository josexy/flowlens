package systemproxy

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

type Mode string

const (
	ModeHTTP   Mode = "http"
	ModeSOCKS5 Mode = "socks5"
)

var (
	ErrUnsupported       = errors.New("system proxy is not supported on this platform")
	ErrUnsupportedMode   = errors.New("system proxy mode is not supported on this platform")
	ErrChangedExternally = errors.New("system proxy was changed outside FlowLens")
)

type Endpoint struct {
	Mode Mode
	Host string
	Port int
}

func (e Endpoint) Address() string {
	if strings.TrimSpace(e.Host) == "" || e.Port <= 0 {
		return ""
	}
	return string(e.Mode) + "://" + net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

func (e Endpoint) validate() error {
	if e.Mode != ModeHTTP && e.Mode != ModeSOCKS5 {
		return fmt.Errorf("unsupported system proxy mode: %s", e.Mode)
	}
	if strings.TrimSpace(e.Host) == "" {
		return errors.New("system proxy host cannot be empty")
	}
	if e.Port <= 0 || e.Port > 65535 {
		return fmt.Errorf("invalid system proxy port: %d", e.Port)
	}
	return nil
}

type ControllerState struct {
	Supported             bool
	Active                bool
	Endpoint              Endpoint
	OriginalUpstreamProxy string
}

type platformDriver interface {
	Supported() bool
	Supports(Mode) bool
	Snapshot() (any, error)
	Apply(Endpoint) error
	Matches(Endpoint) (bool, error)
	Restore(any) error
}

type Controller struct {
	mu                    sync.Mutex
	driver                platformDriver
	active                bool
	snapshot              any
	applied               Endpoint
	originalUpstreamProxy string
}

func NewController() *Controller {
	return newController(newPlatformDriver())
}

func newController(driver platformDriver) *Controller {
	return &Controller{driver: driver}
}

func (c *Controller) State() ControllerState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stateLocked()
}

func (c *Controller) Supports(mode Mode) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.driver != nil && c.driver.Supported() && c.driver.Supports(mode)
}

func (c *Controller) stateLocked() ControllerState {
	supported := c.driver != nil && c.driver.Supported()
	return ControllerState{
		Supported:             supported,
		Active:                c.active,
		Endpoint:              c.applied,
		OriginalUpstreamProxy: c.originalUpstreamProxy,
	}
}

func (c *Controller) Apply(endpoint Endpoint, originalUpstreamProxy string) error {
	if err := endpoint.validate(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.driver == nil || !c.driver.Supported() {
		return ErrUnsupported
	}
	if !c.driver.Supports(endpoint.Mode) {
		return fmt.Errorf("%w: %s", ErrUnsupportedMode, endpoint.Mode)
	}

	if c.active {
		matches, err := c.driver.Matches(c.applied)
		if err != nil {
			return err
		}
		if !matches {
			c.clearLocked()
			return ErrChangedExternally
		}
		if endpoint == c.applied {
			return nil
		}
		if err := c.driver.Apply(endpoint); err != nil {
			if errors.Is(err, ErrChangedExternally) {
				c.clearLocked()
			}
			return err
		}
		c.applied = endpoint
		return nil
	}

	snapshot, err := c.driver.Snapshot()
	if err != nil {
		return err
	}
	if err := c.driver.Apply(endpoint); err != nil {
		if errors.Is(err, ErrChangedExternally) {
			return err
		}
		if restoreErr := c.driver.Restore(snapshot); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore system proxy after apply failure: %w", restoreErr))
		}
		return err
	}

	c.snapshot = snapshot
	c.applied = endpoint
	c.originalUpstreamProxy = strings.TrimSpace(originalUpstreamProxy)
	c.active = true
	return nil
}

func (c *Controller) Restore() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active {
		return nil
	}

	matches, err := c.driver.Matches(c.applied)
	if err != nil {
		return err
	}
	if !matches {
		c.clearLocked()
		return ErrChangedExternally
	}
	if err := c.driver.Restore(c.snapshot); err != nil {
		if errors.Is(err, ErrChangedExternally) {
			c.clearLocked()
		}
		return err
	}
	c.clearLocked()
	return nil
}

func (c *Controller) clearLocked() {
	c.active = false
	c.snapshot = nil
	c.applied = Endpoint{}
	c.originalUpstreamProxy = ""
}

func ProxyServerValue(endpoint Endpoint) string {
	hostPort := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
	if endpoint.Mode == ModeSOCKS5 {
		return "socks=" + hostPort
	}
	return "http=" + hostPort + ";https=" + hostPort
}
