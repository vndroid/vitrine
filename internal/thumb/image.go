package thumb

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register decoder
	"image/jpeg"
	_ "image/png" // register decoder
	"io"
	"math"
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
// the longer side. EXIF orientation is applied. With useExif the JPEG's
// embedded EXIF thumbnail is used instead of decoding the whole photo,
// if it is large enough and has the photo's aspect ratio.
func render(path string, width, height int, useExif bool) (*image.RGBA, error) {
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
	var src image.Image
	if format == "jpeg" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		info := jpegExif(f)
		o = info.orientation
		if useExif && height != 0 && info.thumb != nil {
			src = embeddedThumb(info, cfg, width, height)
		}
	}
	if src == nil {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if src, _, err = image.Decode(f); err != nil {
			return nil, errSource
		}
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

// embeddedThumb decodes the EXIF thumbnail if it covers width x height
// (no upscaling) and has the aspect ratio of the photo (no black bars).
func embeddedThumb(info exifInfo, photo image.Config, width, height int) image.Image {
	img, err := jpeg.Decode(bytes.NewReader(info.thumb))
	if err != nil {
		return nil
	}
	tw, th := img.Bounds().Dx(), img.Bounds().Dy()
	if tw <= 0 || th <= 0 {
		return nil
	}
	dw, dh := tw, th // displayed size
	if swapsAxes(info.orientation) {
		dw, dh = th, tw
	}
	if dw < width || dh < height {
		return nil
	}
	photoR := float64(photo.Width) / float64(photo.Height)
	if math.Abs(float64(tw)/float64(th)-photoR)/photoR > 0.02 {
		return nil
	}
	return img
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
