package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	appfs "github.com/josexy/flowlens/backend/pkg/fs"
)

const (
	resetPrepared = "prepared"
	resetPending  = "pending"
	resetCleaned  = "cleaned"
)

// The marker lives outside the data directory so partial deletion cannot erase
// the user's confirmed intent. It contains no arbitrary data-directory override.
type resetManifest struct {
	Version       int      `json:"version"`
	Phase         string   `json:"phase"`
	Token         string   `json:"token"`
	Language      string   `json:"language"`
	UpdateStaging []string `json:"updateStaging,omitempty"`
}

func resetDirectory() (string, error) {
	base, err := appfs.BaseStoragePath()
	if err != nil {
		return "", err
	}
	return base + ".reset", nil
}

func validateResetManifest(manifest *resetManifest) error {
	if manifest.Version != 1 || (manifest.Phase != resetPrepared && manifest.Phase != resetPending && manifest.Phase != resetCleaned) {
		return errors.New("unsupported application reset marker")
	}
	if _, err := uuid.Parse(manifest.Token); err != nil || len(manifest.Token) != 36 {
		return errors.New("invalid application reset token")
	}
	if manifest.Language != "zh" && manifest.Language != "en" {
		return errors.New("invalid application reset language")
	}
	for _, path := range manifest.UpdateStaging {
		if !isOwnedUpdateStaging(path) {
			return errors.New("invalid application reset staging directory")
		}
	}
	return nil
}

func isOwnedUpdateStaging(path string) bool {
	if !filepath.IsAbs(path) || !strings.HasPrefix(filepath.Base(path), "wails-update-") {
		return false
	}
	parent, err := filepath.Abs(os.TempDir())
	return err == nil && sameResetPath(filepath.Dir(path), parent)
}

// downloaded paths may point inside an extracted .app bundle. Only retain the
// exact immediate temp child containing this updater's known staged artifact.
func resetUpdateStaging(downloaded string) []string {
	if downloaded == "" {
		return nil
	}
	parent, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil
	}
	relative, err := filepath.Rel(parent, downloaded)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	path := filepath.Join(parent, strings.Split(relative, string(filepath.Separator))[0])
	if !isOwnedUpdateStaging(path) {
		return nil
	}
	return []string{path}
}

func sameResetPath(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func saveResetManifest(manifest *resetManifest) error {
	if err := validateResetManifest(manifest); err != nil {
		return err
	}
	directory, err := resetDirectory()
	if err != nil {
		return err
	}
	if err := rejectResetLink(directory); err != nil {
		return err
	}
	if err := appfs.EnsurePrivateDir(directory); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(manifest); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, "manifest.json"))
}

func loadResetManifest() (*resetManifest, error) {
	directory, err := resetDirectory()
	if err != nil {
		return nil, err
	}
	if err := rejectResetLink(directory); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "manifest.json")
	if err := rejectResetLink(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	manifest := new(resetManifest)
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	if err := decoder.Decode(manifest); err != nil {
		return nil, err
	}
	if err := validateResetManifest(manifest); err != nil {
		return nil, err
	}
	return manifest, nil
}

func rejectResetLink(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to reset a linked path: %s", path)
	}
	return nil
}

func clearResetMarker() error {
	directory, err := resetDirectory()
	if err != nil {
		return err
	}
	if err := rejectResetLink(directory); err != nil {
		return err
	}
	return removeResetPath(context.Background(), directory, os.RemoveAll, 50, 100*time.Millisecond)
}

func resetCleanupPaths(manifest *resetManifest) ([]string, error) {
	if err := validateResetManifest(manifest); err != nil {
		return nil, err
	}
	base, err := appfs.BaseStoragePath()
	if err != nil {
		return nil, err
	}
	base, err = filepath.Abs(base)
	if err != nil || filepath.Dir(base) == base {
		return nil, errors.New("invalid application reset storage root")
	}
	paths := []string{base}
	paths = append(paths, manifest.UpdateStaging...)
	return paths, nil
}

func cleanResetData(manifest *resetManifest, remove func(string) error, attempts int, retryDelay time.Duration) error {
	return cleanResetDataWithProgress(context.Background(), manifest, remove, attempts, retryDelay, nil)
}

func cleanResetDataWithProgress(ctx context.Context, manifest *resetManifest, remove func(string) error, attempts int, retryDelay time.Duration, progress func(string)) error {
	if manifest == nil {
		return errors.New("application reset marker is unavailable")
	}
	if attempts < 1 {
		return errors.New("application reset requires at least one cleanup attempt")
	}
	if manifest.Phase == resetCleaned {
		return nil
	}
	if manifest.Phase != resetPending {
		return errors.New("application reset has not been committed")
	}
	paths, err := resetCleanupPaths(manifest)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := rejectResetLink(path); err != nil {
			return err
		}
	}
	for index, path := range paths {
		if progress != nil {
			stage := "temporary"
			if index == 0 {
				stage = "storage"
			}
			progress(stage)
		}
		if err := removeResetPath(ctx, path, remove, attempts, retryDelay); err != nil {
			return fmt.Errorf("remove application data %q: %w", path, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest.Phase = resetCleaned
	if err := saveResetManifest(manifest); err != nil {
		manifest.Phase = resetPending
		return err
	}
	return nil
}

func removeResetPath(ctx context.Context, path string, remove func(string) error, attempts int, retryDelay time.Duration) error {
	for attempt := range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Revalidate each attempt; never retry a path replaced with a link.
		if err := rejectResetLink(path); err != nil {
			return err
		}
		err := remove(path)
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if attempt+1 == attempts || !isRetryableResetError(err) {
			return err
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("application reset requires at least one cleanup attempt")
}

func isRetryableResetError(err error) bool {
	return errors.Is(err, syscall.EBUSY) || isResetSharingViolation(err)
}
