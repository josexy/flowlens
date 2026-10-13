package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	appfs "github.com/josexy/flowlens/backend/pkg/fs"
	appservice "github.com/josexy/flowlens/backend/services/app_service"
)

const resetStartupEnvironment = "FLOWLENS_RESET_STARTUP_DIR"

// The UI has its own process. The application process keeps the single-instance
// lock and opens no database until cleanup has succeeded.
type resetStartupSnapshot struct {
	appservice.ResetStartupState
	Token     string `json:"token"`
	UpdatedAt int64  `json:"updatedAt"`
}

type resetStartupAction struct {
	Sequence uint64 `json:"sequence"`
	Action   string `json:"action"`
	Ready    bool   `json:"ready,omitempty"`
}

type resetStartupClient struct {
	directory     string
	command       *exec.Cmd
	ctx           context.Context
	cancel        context.CancelFunc
	done          chan struct{}
	ready         chan struct{}
	actions       chan string
	mu            sync.Mutex
	state         resetStartupSnapshot
	closing       bool
	closeOnce     sync.Once
	closeErr      error
	exitRequested atomic.Bool
}

func writeResetStartupJSON(directory, name string, value any) error {
	file, err := os.CreateTemp(directory, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	for attempt := range 10 {
		err := os.Rename(file.Name(), filepath.Join(directory, name))
		if err == nil || !isResetStartupReplaceBlocked(err) || attempt == 9 {
			return err
		}
		// MoveFileEx can reject replacement during a brief Windows read,
		// including a polling reader outside this application.
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func readResetStartupJSON(directory, name string, value any) error {
	file, err := openResetStartupFile(filepath.Join(directory, name))
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

// Reset errors use the same startup window as cleanup. Nothing after this
// function may open the database until a valid pending reset has been cleaned.
func prepareResetStartup() (*resetManifest, *resetStartupClient, error) {
	for {
		manifest, failure := loadResetManifest()
		if failure == nil && manifest != nil && manifest.Phase == resetPrepared {
			// The old process never committed this reset. Clear only its marker.
			failure = clearResetMarker()
			if failure == nil {
				return nil, nil, nil
			}
		}
		if failure == nil && manifest == nil {
			return nil, nil, nil
		}
		client, err := startResetStartup(manifest, failure)
		if err != nil {
			return nil, nil, err
		}
		manifest, err = client.waitForManifest(manifest, failure)
		if err != nil {
			return nil, client, err
		}
		if manifest.Phase == resetPrepared {
			// A repaired, uncommitted marker must not authorise data deletion.
			// Close the helper before removing its private session directory.
			if err := client.Close(); err != nil {
				return nil, nil, err
			}
			if err := clearResetMarker(); err != nil {
				continue
			}
			return nil, nil, nil
		}
		client.mu.Lock()
		client.state.Language = manifest.Language
		client.state.Stages = resetStartupStages(manifest)
		client.mu.Unlock()
		if manifest.Phase == resetPending {
			err = client.clean(manifest)
		} else {
			client.update("initializing", "", true)
			err = client.ctx.Err()
		}
		return manifest, client, err
	}
}

func (c *resetStartupClient) waitForManifest(manifest *resetManifest, failure error) (*resetManifest, error) {
	if failure == nil {
		// The UI may take time to load. Revalidate the persisted intent before
		// accepting it, rather than relying on the earlier startup snapshot.
		manifest, failure = loadResetManifest()
		if failure == nil && manifest == nil {
			failure = errors.New("application reset marker is unavailable")
		}
	}
	for failure != nil {
		c.update("starting", failure.Error(), false)
		select {
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		case <-c.actions:
			manifest, failure = loadResetManifest()
			if failure == nil && manifest == nil {
				failure = errors.New("application reset marker is unavailable")
			}
		}
	}
	return manifest, nil
}

func startResetStartup(manifest *resetManifest, failure error) (*resetStartupClient, error) {
	root, err := resetDirectory()
	if err != nil {
		return nil, err
	}
	root = filepath.Join(root, "startup")
	if err := rejectResetLink(root); err != nil {
		return nil, err
	}
	directory := filepath.Join(root, uuid.NewString())
	if err := appfs.EnsurePrivateDir(directory); err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	state := appservice.ResetStartupState{Language: "zh", Stage: "starting", Busy: failure == nil}
	if manifest != nil {
		state.Language, state.Stages = manifest.Language, resetStartupStages(manifest)
	}
	if failure != nil {
		state.Error = failure.Error()
	}
	client := &resetStartupClient{
		directory: directory, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), ready: make(chan struct{}), actions: make(chan string, 1),
		state: resetStartupSnapshot{
			ResetStartupState: state,
			Token:             filepath.Base(directory),
		},
	}
	if err := client.publish(); err != nil {
		cancel()
		return nil, err
	}
	command := exec.Command(executable)
	command.Stderr = os.Stderr
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name != resetStartupEnvironment {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, resetStartupEnvironment+"="+directory)
	configureResetStartupCommand(command)
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start reset progress window: %w", err)
	}
	client.command = command
	go func() {
		_ = command.Wait()
		close(client.done)
	}()
	go client.monitor()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	select {
	case <-client.ready:
		return client, nil
	case <-client.ctx.Done():
		_ = client.Close()
		if client.exitRequested.Load() {
			return nil, context.Canceled
		}
		return nil, errors.New("reset progress window closed before becoming ready")
	case <-timer.C:
		_ = client.Close()
		return nil, errors.New("reset progress window did not become ready")
	}
}

func resetStartupStages(manifest *resetManifest) []string {
	if manifest == nil {
		return nil
	}
	stages := []string{"storage"}
	if len(manifest.UpdateStaging) > 0 {
		stages = append(stages, "temporary")
	}
	return append(stages, "initializing")
}

func (c *resetStartupClient) publish() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.UpdatedAt = time.Now().UnixMilli()
	return writeResetStartupJSON(c.directory, "state.json", c.state)
}

func (c *resetStartupClient) update(stage, failure string, busy bool) {
	c.mu.Lock()
	if stage != "" {
		c.state.Stage = stage
	}
	c.state.Error, c.state.Busy = failure, busy
	c.mu.Unlock()
	if err := c.publish(); err != nil {
		c.cancel()
	}
}

func (c *resetStartupClient) monitor() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var sequence uint64
	var readyOnce sync.Once
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.done:
			c.mu.Lock()
			closing := c.closing
			c.mu.Unlock()
			if !closing {
				c.cancel()
			}
			return
		case <-ticker.C:
			var action resetStartupAction
			err := readResetStartupJSON(c.directory, "action.json", &action)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				c.cancel()
				return
			}
			if err == nil && action.Sequence > sequence {
				sequence = action.Sequence
				// A quick retry can replace the ready action before this poll.
				// Every later UI action carries the rendered-frame handshake.
				if action.Ready {
					readyOnce.Do(func() { close(c.ready) })
				}
				switch action.Action {
				case "ready":
					readyOnce.Do(func() { close(c.ready) })
				case "exit":
					c.exitRequested.Store(true)
					c.cancel()
					return
				case "retry":
					c.mu.Lock()
					canRetry := !c.state.Busy && c.state.Error != "" && !c.closing
					if canRetry {
						c.state.Busy = true
					}
					c.mu.Unlock()
					if canRetry {
						select {
						case c.actions <- "retry":
						default:
						}
					}
				}
			}
			if err := c.publish(); err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (c *resetStartupClient) clean(manifest *resetManifest) error {
	for {
		err := cleanResetDataWithProgress(c.ctx, manifest, os.RemoveAll, 50, 100*time.Millisecond, func(stage string) {
			c.update(stage, "", true)
		})
		if err == nil {
			c.update("initializing", "", true)
			return c.ctx.Err()
		}
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		// Keep the active stage so the UI can identify the failed step and
		// retain completed steps even when updates occur between polls.
		c.update("", err.Error(), false)
		select {
		case <-c.ctx.Done():
			return c.ctx.Err()
		case <-c.actions:
			c.update("starting", "", true)
		}
	}
}

func (c *resetStartupClient) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closing, c.state.Close = true, true
		c.mu.Unlock()
		c.closeErr = c.publish()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-c.done:
		case <-timer.C:
			_ = c.command.Process.Kill()
			<-c.done
			c.closeErr = errors.New("reset progress window did not close normally")
		}
		c.cancel()
	})
	return c.closeErr
}

func resetStartupSessionDirectory() (string, error) {
	directory := os.Getenv(resetStartupEnvironment)
	root, err := resetDirectory()
	if err != nil {
		return "", err
	}
	root = filepath.Join(root, "startup")
	if !filepath.IsAbs(directory) || directory != filepath.Clean(directory) || !sameResetPath(filepath.Dir(directory), root) {
		return "", errors.New("invalid reset startup directory")
	}
	if _, err := uuid.Parse(filepath.Base(directory)); err != nil {
		return "", errors.New("invalid reset startup session")
	}
	for _, path := range []string{filepath.Dir(root), root, directory} {
		if err := rejectResetLink(path); err != nil {
			return "", err
		}
	}
	return directory, nil
}
