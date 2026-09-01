package processattribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	appfs "github.com/josexy/flowlens/backend/pkg/fs"
)

const (
	processIconSize       = 64
	maxProcessIconPNGSize = 256 << 10
)

type IconStore struct {
	mu  sync.RWMutex
	dir string
}

func NewIconStore(root string) (*IconStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("process icon storage root cannot be empty")
	}
	return &IconStore{dir: filepath.Join(root, "cache", "process-icons")}, nil
}

func (s *IconStore) Put(source image.Image) (string, error) {
	if s == nil {
		return "", errors.New("process icon store is not available")
	}
	if source == nil {
		return "", errors.New("process icon source cannot be nil")
	}

	// Put and Get may run concurrently because icons are installed with an
	// atomic rename. Reset takes the exclusive lock and therefore still waits
	// for every active read or write to finish before removing the directory.
	s.mu.RLock()
	defer s.mu.RUnlock()

	normalized, err := normalizeProcessIcon(source)
	if err != nil {
		return "", err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, normalized); err != nil {
		return "", fmt.Errorf("encode process icon PNG: %w", err)
	}
	if encoded.Len() > maxProcessIconPNGSize {
		return "", fmt.Errorf("encoded process icon exceeds %d bytes", maxProcessIconPNGSize)
	}
	sum := sha256.Sum256(encoded.Bytes())
	key := hex.EncodeToString(sum[:])

	if err := appfs.EnsurePrivateDir(s.dir); err != nil {
		return "", fmt.Errorf("create process icon cache: %w", err)
	}
	destination := filepath.Join(s.dir, key+".png")
	if info, err := os.Stat(destination); err == nil && info.Mode().IsRegular() {
		if err := appfs.EnsurePrivateFile(destination); err != nil {
			return "", fmt.Errorf("secure cached process icon: %w", err)
		}
		return key, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect cached process icon: %w", err)
	}

	temporary, err := os.CreateTemp(s.dir, ".process-icon-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary process icon: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if _, err := temporary.Write(encoded.Bytes()); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write temporary process icon: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync temporary process icon: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary process icon: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		if info, statErr := os.Stat(destination); statErr == nil && info.Mode().IsRegular() {
			if secureErr := appfs.EnsurePrivateFile(destination); secureErr != nil {
				return "", fmt.Errorf("secure concurrently cached process icon: %w", secureErr)
			}
			return key, nil
		}
		return "", fmt.Errorf("install process icon: %w", err)
	}
	keepTemporary = false
	return key, nil
}

func (s *IconStore) Get(key string) ([]byte, bool, error) {
	if s == nil {
		return nil, false, errors.New("process icon store is not available")
	}
	if !isValidIconKey(key) {
		return nil, false, errors.New("invalid process icon key")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	data, err := os.ReadFile(filepath.Join(s.dir, key+".png"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read process icon: %w", err)
	}
	return data, true, nil
}

func (s *IconStore) Reset() error {
	if s == nil {
		return errors.New("process icon store is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("reset process icon cache: %w", err)
	}
	return nil
}

func isValidIconKey(key string) bool {
	if len(key) != sha256.Size*2 {
		return false
	}
	for _, char := range key {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func normalizeProcessIcon(source image.Image) (*image.NRGBA, error) {
	bounds := source.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return nil, errors.New("process icon has empty bounds")
	}

	targetWidth := processIconSize
	targetHeight := processIconSize
	if width > height {
		targetHeight = max(1, int(int64(height)*processIconSize/int64(width)))
	} else if height > width {
		targetWidth = max(1, int(int64(width)*processIconSize/int64(height)))
	}
	offsetX := (processIconSize - targetWidth) / 2
	offsetY := (processIconSize - targetHeight) / 2
	destination := image.NewNRGBA(image.Rect(0, 0, processIconSize, processIconSize))

	for y := range targetHeight {
		sourceY := bounds.Min.Y + min(height-1, int((int64(y)*int64(height))/int64(targetHeight)))
		for x := range targetWidth {
			sourceX := bounds.Min.X + min(width-1, int((int64(x)*int64(width))/int64(targetWidth)))
			pixel := color.NRGBAModel.Convert(source.At(sourceX, sourceY)).(color.NRGBA)
			destination.SetNRGBA(offsetX+x, offsetY+y, pixel)
		}
	}
	return destination, nil
}
