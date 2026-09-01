//go:build linux

package processattribution

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
)

const (
	linuxIconTargetSize   = 64
	linuxIconFileMaxBytes = 8 << 20
)

type linuxIconThemeDirectory struct {
	path      string
	minSize   int
	maxSize   int
	priority  int
	directory int
}

type linuxIconCandidate struct {
	path          string
	rootPriority  int
	themePriority int
	directory     int
	belowTarget   bool
	distance      int
	format        int
}

func resolveLinuxIconPath(icon string, dataDirs []string) (string, error) {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return "", &IconUnavailableError{Reason: "linux_icon_name_unavailable"}
	}
	if filepath.IsAbs(icon) {
		if err := validateLinuxIconFile(icon); err != nil {
			return "", &IconUnavailableError{Reason: "linux_absolute_icon_unavailable"}
		}
		return icon, nil
	}
	if strings.ContainsAny(icon, `/\\`) || icon == "." || icon == ".." {
		return "", &IconUnavailableError{Reason: "linux_icon_name_invalid"}
	}
	name := icon
	if extension := strings.ToLower(filepath.Ext(name)); extension == ".png" || extension == ".svg" || extension == ".xpm" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if name == "" {
		return "", &IconUnavailableError{Reason: "linux_icon_name_invalid"}
	}

	themes := []string{"hicolor"}
	visited := make(map[string]struct{})
	for themePriority := 0; len(themes) > 0; themePriority++ {
		theme := strings.TrimSpace(themes[0])
		themes = themes[1:]
		if theme == "" {
			continue
		}
		if _, ok := visited[theme]; ok {
			continue
		}
		visited[theme] = struct{}{}
		candidates, inherited := findLinuxThemeIconCandidates(theme, name, dataDirs, themePriority)
		if len(candidates) > 0 {
			sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].less(candidates[j]) })
			return candidates[0].path, nil
		}
		themes = append(themes, inherited...)
	}

	var pixmapCandidates []linuxIconCandidate
	for rootPriority, dataDir := range dataDirs {
		for _, extension := range []string{".png", ".svg", ".xpm"} {
			path := filepath.Join(dataDir, "pixmaps", name+extension)
			if validateLinuxIconFile(path) != nil {
				continue
			}
			pixmapCandidates = append(pixmapCandidates, linuxIconCandidate{
				path:         path,
				rootPriority: rootPriority,
				format:       linuxIconFormatPriority(extension),
			})
		}
	}
	if len(pixmapCandidates) > 0 {
		sort.SliceStable(pixmapCandidates, func(i, j int) bool { return pixmapCandidates[i].less(pixmapCandidates[j]) })
		return pixmapCandidates[0].path, nil
	}
	return "", &IconUnavailableError{Reason: "linux_icon_not_found"}
}

func findLinuxThemeIconCandidates(theme, name string, dataDirs []string, themePriority int) ([]linuxIconCandidate, []string) {
	var candidates []linuxIconCandidate
	var inherited []string
	seenInherited := make(map[string]struct{})
	metadata := make([]map[string]map[string]string, len(dataDirs))
	var fallbackMetadata map[string]map[string]string
	for rootPriority, dataDir := range dataDirs {
		themeRoot := filepath.Join(dataDir, "icons", theme)
		sections, err := parseLinuxINIFile(filepath.Join(themeRoot, "index.theme"), linuxDesktopFileMaxBytes)
		if err != nil {
			continue
		}
		metadata[rootPriority] = sections
		if fallbackMetadata == nil {
			fallbackMetadata = sections
		}
		iconTheme := sections["Icon Theme"]
		for _, parent := range splitLinuxList(iconTheme["Inherits"]) {
			if _, ok := seenInherited[parent]; ok {
				continue
			}
			seenInherited[parent] = struct{}{}
			inherited = append(inherited, parent)
		}
	}
	for rootPriority, dataDir := range dataDirs {
		sections := metadata[rootPriority]
		if sections == nil {
			sections = fallbackMetadata
		}
		if sections == nil {
			continue
		}
		themeRoot := filepath.Join(dataDir, "icons", theme)
		for directoryIndex, directoryName := range splitLinuxList(sections["Icon Theme"]["Directories"]) {
			directory := parseLinuxIconThemeDirectory(directoryName, sections[directoryName], rootPriority, directoryIndex)
			if directory.maxSize <= 0 {
				continue
			}
			for _, extension := range []string{".png", ".svg", ".xpm"} {
				path := filepath.Join(themeRoot, filepath.FromSlash(directory.path), name+extension)
				if validateLinuxIconFile(path) != nil {
					continue
				}
				below := directory.maxSize < linuxIconTargetSize
				distance := 0
				switch {
				case directory.minSize > linuxIconTargetSize:
					distance = directory.minSize - linuxIconTargetSize
				case directory.maxSize < linuxIconTargetSize:
					distance = linuxIconTargetSize - directory.maxSize
				}
				candidates = append(candidates, linuxIconCandidate{
					path:          path,
					rootPriority:  rootPriority,
					themePriority: themePriority,
					directory:     directory.directory,
					belowTarget:   below,
					distance:      distance,
					format:        linuxIconFormatPriority(extension),
				})
			}
		}
	}
	return candidates, inherited
}

func parseLinuxIconThemeDirectory(path string, values map[string]string, priority, directory int) linuxIconThemeDirectory {
	size, _ := strconv.Atoi(values["Size"])
	scale, _ := strconv.Atoi(values["Scale"])
	if scale <= 0 {
		scale = 1
	}
	size *= scale
	minimum, maximum := size, size
	switch strings.ToLower(values["Type"]) {
	case "scalable":
		if parsed, err := strconv.Atoi(values["MinSize"]); err == nil && parsed > 0 {
			minimum = parsed * scale
		}
		if parsed, err := strconv.Atoi(values["MaxSize"]); err == nil && parsed > 0 {
			maximum = parsed * scale
		}
	case "threshold", "":
		threshold := 2
		if parsed, err := strconv.Atoi(values["Threshold"]); err == nil && parsed >= 0 {
			threshold = parsed
		}
		minimum = max(1, size-threshold*scale)
		maximum = size + threshold*scale
	default:
		minimum = size
		maximum = size
	}
	return linuxIconThemeDirectory{path: path, minSize: minimum, maxSize: maximum, priority: priority, directory: directory}
}

func splitLinuxList(value string) []string {
	return strings.FieldsFunc(value, func(character rune) bool {
		return character == ',' || character == ';'
	})
}

func (candidate linuxIconCandidate) less(other linuxIconCandidate) bool {
	if candidate.rootPriority != other.rootPriority {
		return candidate.rootPriority < other.rootPriority
	}
	if candidate.themePriority != other.themePriority {
		return candidate.themePriority < other.themePriority
	}
	if candidate.belowTarget != other.belowTarget {
		return !candidate.belowTarget
	}
	if candidate.distance != other.distance {
		return candidate.distance < other.distance
	}
	if candidate.format != other.format {
		return candidate.format < other.format
	}
	if candidate.directory != other.directory {
		return candidate.directory < other.directory
	}
	return candidate.path < other.path
}

func linuxIconFormatPriority(extension string) int {
	switch strings.ToLower(extension) {
	case ".png":
		return 0
	case ".svg":
		return 1
	default:
		return 2
	}
}

func validateLinuxIconFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > linuxIconFileMaxBytes {
		return errors.New("Linux icon is not a bounded regular file")
	}
	return nil
}

func loadLinuxIconFile(path string) (image.Image, error) {
	if err := validateLinuxIconFile(path); err != nil {
		return nil, &IconUnavailableError{Reason: "linux_icon_file_unavailable"}
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		config, err := png.DecodeConfig(file)
		if err != nil {
			return nil, fmt.Errorf("decode Linux PNG icon configuration: %w", err)
		}
		if config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16<<20 {
			return nil, &IconUnavailableError{Reason: "linux_png_dimensions_unsupported"}
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		decoded, err := png.Decode(file)
		if err != nil {
			return nil, fmt.Errorf("decode Linux PNG icon: %w", err)
		}
		return decoded, nil
	case ".svg":
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		icon, err := oksvg.ReadReplacingCurrentColor(bytes.NewReader(data), "#808080", oksvg.StrictErrorMode)
		if err != nil {
			return nil, &IconUnavailableError{Reason: "linux_svg_unsupported"}
		}
		if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 || math.IsNaN(icon.ViewBox.W) || math.IsNaN(icon.ViewBox.H) ||
			math.IsInf(icon.ViewBox.W, 0) || math.IsInf(icon.ViewBox.H, 0) {
			return nil, &IconUnavailableError{Reason: "linux_svg_invalid_bounds"}
		}
		result := image.NewRGBA(image.Rect(0, 0, linuxIconTargetSize, linuxIconTargetSize))
		scale := math.Min(float64(linuxIconTargetSize)/icon.ViewBox.W, float64(linuxIconTargetSize)/icon.ViewBox.H)
		width := icon.ViewBox.W * scale
		height := icon.ViewBox.H * scale
		icon.SetTarget((float64(linuxIconTargetSize)-width)/2, (float64(linuxIconTargetSize)-height)/2, width, height)
		scanner := rasterx.NewScannerGV(linuxIconTargetSize, linuxIconTargetSize, result, result.Bounds())
		raster := rasterx.NewDasher(linuxIconTargetSize, linuxIconTargetSize, scanner)
		icon.Draw(raster, 1)
		return result, nil
	case ".xpm":
		return nil, &IconUnavailableError{Reason: "linux_xpm_unsupported"}
	default:
		return nil, &IconUnavailableError{Reason: "linux_icon_format_unsupported"}
	}
}
