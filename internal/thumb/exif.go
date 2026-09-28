package thumb

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	"io"
)

// jpegOrientation reads the EXIF orientation (1-8) of a JPEG, 1 if absent.
func jpegOrientation(r io.Reader) int {
	br := bufio.NewReader(io.LimitReader(r, 1<<20))
	var soi [2]byte
	if _, err := io.ReadFull(br, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return 1
	}
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(br, hdr[:2]); err != nil || hdr[0] != 0xFF {
			return 1
		}
		marker := hdr[1]
		if marker == 0xD8 || marker >= 0xD0 && marker <= 0xD7 || marker == 0x01 {
			continue // markers without length
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan, end of image
			return 1
		}
		if _, err := io.ReadFull(br, hdr[2:]); err != nil {
			return 1
		}
		size := int(binary.BigEndian.Uint16(hdr[2:])) - 2
		if size < 0 {
			return 1
		}
		if marker != 0xE1 {
			if _, err := br.Discard(size); err != nil {
				return 1
			}
			continue
		}
		seg := make([]byte, size)
		if _, err := io.ReadFull(br, seg); err != nil {
			return 1
		}
		if o := exifOrientation(seg); o != 0 {
			return o
		}
	}
}

// exifOrientation parses an APP1 segment ("Exif\0\0" + TIFF).
func exifOrientation(seg []byte) int {
	tiff, ok := bytes.CutPrefix(seg, []byte("Exif\x00\x00"))
	if !ok || len(tiff) < 8 {
		return 0
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	ifd := int(bo.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	n := int(bo.Uint16(tiff[ifd:]))
	for i := 0; i < n; i++ {
		e := ifd + 2 + i*12
		if e+12 > len(tiff) {
			return 0
		}
		if bo.Uint16(tiff[e:]) == 0x0112 && bo.Uint16(tiff[e+2:]) == 3 {
			if o := int(bo.Uint16(tiff[e+8:])); o >= 1 && o <= 8 {
				return o
			}
		}
	}
	return 0
}

// swapsAxes reports whether an orientation turns the image by 90 degrees.
func swapsAxes(o int) bool { return o >= 5 && o <= 8 }

// toStored maps a point of the displayed (oriented) image back to the
// stored image of size w x h (continuous coordinates).
func toStored(o int, ox, oy, w, h float64) (float64, float64) {
	switch o {
	case 2:
		return w - ox, oy
	case 3:
		return w - ox, h - oy
	case 4:
		return ox, h - oy
	case 5:
		return oy, ox
	case 6:
		return oy, h - ox
	case 7:
		return w - oy, h - ox
	case 8:
		return w - oy, ox
	}
	return ox, oy
}

// storedRect maps a rectangle of the displayed image to the stored image.
func storedRect(o int, x0, y0, x1, y1 float64, w, h int) image.Rectangle {
	ax, ay := toStored(o, x0, y0, float64(w), float64(h))
	bx, by := toStored(o, x1, y1, float64(w), float64(h))
	return image.Rect(int(min(ax, bx)+0.5), int(min(ay, by)+0.5), int(max(ax, bx)+0.5), int(max(ay, by)+0.5)).Intersect(image.Rect(0, 0, w, h))
}

// orient applies an orientation to a (small) image.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if swapsAxes(o) {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for oy := 0; oy < dh; oy++ {
		for ox := 0; ox < dw; ox++ {
			var x, y int
			switch o {
			case 2:
				x, y = w-1-ox, oy
			case 3:
				x, y = w-1-ox, h-1-oy
			case 4:
				x, y = ox, h-1-oy
			case 5:
				x, y = oy, ox
			case 6:
				x, y = oy, h-1-ox
			case 7:
				x, y = w-1-oy, h-1-ox
			case 8:
				x, y = w-1-oy, ox
			}
			si := src.PixOffset(x, y)
			di := dst.PixOffset(ox, oy)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
