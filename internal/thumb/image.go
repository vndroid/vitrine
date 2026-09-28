package thumb

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register decoder
	"image/jpeg"
	_ "image/png" // register decoder
	"io"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"

	_ "golang.org/x/image/bmp"  // register decoder
	_ "golang.org/x/image/webp" // register decoder
)

// Source limits, like h5fs: bounded memory per decode.
const (
	maxSourceBytes     = 32 << 20
	maxSourceDimension = 16384
	maxSourcePixels    = 25_000_000
	jpegQuality        = 80
)

var errSource = errors.New("thumb: unsupported or too large image")

// render decodes an image and returns a width x height thumbnail, cropped
// like h5fs (horizontally centered, top aligned) on a white background.
// A height of 0 returns a proportional sample of at most width pixels on
// the longer side. EXIF orientation is applied.
func render(path string, width, height int) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.Size() <= 0 || fi.Size() > maxSourceBytes {
		return nil, errSource
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > maxSourceDimension || cfg.Height > maxSourceDimension ||
		cfg.Width*cfg.Height > maxSourcePixels {
		return nil, errSource
	}
	o := 1
	if format == "jpeg" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		o = jpegOrientation(f)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, errSource
	}

	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	ow, oh := float64(sw), float64(sh) // displayed size
	if swapsAxes(o) {
		ow, oh = oh, ow
	}

	srcR := ow / oh
	w, h := float64(width), float64(height)
	if height == 0 {
		if srcR >= 1 {
			h = w / srcR
		} else {
			h = w
			w = h * srcR
		}
		if w > ow {
			w, h = ow, oh
		}
	}
	ratio := w / h
	var cx, cw, ch float64
	if srcR <= ratio {
		cw = ow
		ch = cw / ratio
	} else {
		ch = oh
		cw = ch * ratio
		cx = 0.5 * (ow - cw)
	}
	dw, dh := max(int(w), 1), max(int(h), 1)

	crop := storedRect(o, cx, 0, cx+cw, ch, sw, sh).Add(src.Bounds().Min)
	pw, ph := dw, dh
	if swapsAxes(o) {
		pw, ph = dh, dw
	}
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Over, nil)
	return orient(dst, o), nil
}

func writeJPEG(path string, img image.Image) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.jpg")
	if err != nil {
		return err
	}
	if err := jpeg.Encode(tmp, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	os.Chmod(tmp.Name(), 0o644)
	return os.Rename(tmp.Name(), path)
}
