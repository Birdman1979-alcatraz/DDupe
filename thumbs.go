package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"runtime"
	"sync"
)

// Thumbnails are made on demand (only for photos that are shown) and kept in
// memory, up to a limit, so scrolling back is instant.

const thumbMax = 420

var (
	thumbMu    sync.Mutex
	thumbCache = map[string][]byte{}
	thumbOrder []string
	thumbSem   = make(chan struct{}, max(2, runtime.NumCPU()-1))
)

func thumbnail(ph *photo) ([]byte, error) {
	key := fmt.Sprintf("%s|%d|%d", ph.Path, ph.Size, ph.ModTime.UnixNano())
	thumbMu.Lock()
	if b, ok := thumbCache[key]; ok {
		thumbMu.Unlock()
		return b, nil
	}
	thumbMu.Unlock()

	thumbSem <- struct{}{}
	defer func() { <-thumbSem }()

	f, err := os.Open(ph.Path)
	if err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	small := orientRGBA(shrink(img, thumbMax), ph.Orient)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, small, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	b := buf.Bytes()

	thumbMu.Lock()
	if _, ok := thumbCache[key]; !ok {
		thumbCache[key] = b
		thumbOrder = append(thumbOrder, key)
		for len(thumbOrder) > 3000 {
			delete(thumbCache, thumbOrder[0])
			thumbOrder = thumbOrder[1:]
		}
	}
	thumbMu.Unlock()
	return b, nil
}

// shrink scales the image so its longest side is at most maxSide, averaging a
// 3x3 grid of samples per output pixel (fast and smooth enough for previews).
func shrink(img image.Image, maxSide int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := w, h
	if w >= h && w > maxSide {
		tw, th = maxSide, max(1, h*maxSide/w)
	} else if h > w && h > maxSide {
		tw, th = max(1, w*maxSide/h), maxSide
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	ycc, isYCC := img.(*image.YCbCr)
	for y := 0; y < th; y++ {
		for x := 0; x < tw; x++ {
			var r, g, bl, n int
			for sy := 0; sy < 3; sy++ {
				py := b.Min.Y + ((y*3+sy)*h+h/2)/(th*3)
				if py >= b.Max.Y {
					py = b.Max.Y - 1
				}
				for sx := 0; sx < 3; sx++ {
					px := b.Min.X + ((x*3+sx)*w+w/2)/(tw*3)
					if px >= b.Max.X {
						px = b.Max.X - 1
					}
					var cr, cg, cb uint8
					if isYCC {
						yi := ycc.YOffset(px, py)
						ci := ycc.COffset(px, py)
						cr, cg, cb = color.YCbCrToRGB(ycc.Y[yi], ycc.Cb[ci], ycc.Cr[ci])
					} else {
						rr, gg, bb, _ := img.At(px, py).RGBA()
						cr, cg, cb = uint8(rr>>8), uint8(gg>>8), uint8(bb>>8)
					}
					r += int(cr)
					g += int(cg)
					bl += int(cb)
					n++
				}
			}
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = uint8(r/n), uint8(g/n), uint8(bl/n), 255
		}
	}
	return out
}

// orientRGBA applies the camera's EXIF rotation so previews appear upright.
func orientRGBA(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si, di := src.PixOffset(sx, sy), dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
