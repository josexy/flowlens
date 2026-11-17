//go:build ignore

// Run from the repository root: go run ./assets/icons/generate.go
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	app, err := os.ReadFile("assets/icons/app-icon.svg")
	if err != nil {
		return err
	}
	tray, err := os.ReadFile("assets/icons/tray-template.svg")
	if err != nil {
		return err
	}
	appPNG, err := renderPNG(app, 1024)
	if err != nil {
		return err
	}
	trayPNG, err := renderPNG(tray, 72)
	if err != nil {
		return err
	}
	// Match MyGo's PNG-backed ICNS entries. Do not use icp4/icp5: macOS
	// interprets those as legacy ARGB, not PNG, and instead gets 1x small
	// icons from ic11/ic12. Render every size directly from the vector.
	var body bytes.Buffer
	for _, entry := range []struct {
		kind string
		size int
	}{
		{"ic11", 32}, {"ic12", 64}, {"ic07", 128}, {"ic13", 256},
		{"ic08", 256}, {"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
	} {
		data := appPNG
		if entry.size != 1024 {
			data, err = renderPNG(app, entry.size)
			if err != nil {
				return err
			}
		}
		body.WriteString(entry.kind)
		body.Write(binary.BigEndian.AppendUint32(nil, uint32(len(data)+8)))
		body.Write(data)
	}
	icns := append([]byte("icns"), binary.BigEndian.AppendUint32(nil, uint32(body.Len()+8))...)
	icns = append(icns, body.Bytes()...)
	for _, output := range []struct {
		path string
		data []byte
	}{
		{"resources/icon.png", appPNG},
		{"assets/macos/app-icon.icns", icns},
		{"resources/trayicon-template.png", trayPNG},
	} {
		if err := os.WriteFile(output.path, output.data, 0o644); err != nil {
			return err
		}
		fmt.Println(output.path)
	}
	return nil
}

func renderPNG(source []byte, size int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(source), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	// Four samples per axis retain smooth curves and diagonals at the target
	// resolution. Filter premultiplied RGBA to avoid dark transparent fringes.
	const samples = 4
	side := size * samples
	high := image.NewRGBA(image.Rect(0, 0, side, side))
	icon.SetTarget(0, 0, float64(side), float64(side))
	// oksvg v0.2.0 transforms path coordinates but leaves stroke widths in
	// device units. These square SVGs have no local transforms; scale their
	// strokes by the same factor so all export sizes match the SVG artwork.
	for i := range icon.SVGPaths {
		icon.SVGPaths[i].LineWidth *= float64(side) / icon.ViewBox.W
	}
	scanner := rasterx.NewScannerGV(side, side, high, high.Bounds())
	icon.Draw(rasterx.NewDasher(side, side, scanner), 1)
	result := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(result, result.Bounds(), high, high.Bounds(), draw.Src, nil)
	var data bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&data, result); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
