package main

import (
	"encoding/binary"
	"image"
	"math"
	"math/bits"
	"sort"
)

// Two fingerprints are computed for each photo from a 32x32 grayscale
// thumbnail (built by averaging every pixel, so a 4000px original and an
// 800px copy produce nearly the same thumbnail):
//   pHash - based on the overall light/dark structure (robust to edits,
//           compression, brightness and colour changes)
//   dHash - based on left-to-right brightness changes (catches fine layout)
// A pair counts as a look-alike only when BOTH fingerprints are close, which
// keeps unrelated photos from being grouped together.

type sensitivity struct {
	Name   string
	PMax   int // max differing bits in pHash (of 64)
	DMax   int // max differing bits in dHash (of 64)
	SumMax int // max combined difference
	Detail string
}

var levels = []sensitivity{
	{"Strict", 8, 8, 14, "only near-identical copies (resized, re-saved, rotated)"},
	{"Normal", 22, 16, 24, "also edited, brightened, stamped, or slightly cropped copies"},
	{"Loose", 26, 22, 30, "also more heavily cropped copies and near-identical shots (check by eye!)"},
}

const thumb = 32

// grayThumb shrinks the image to 32x32 by averaging all pixels in each cell.
func grayThumb(img image.Image) []float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	sums := make([]float64, thumb*thumb)
	counts := make([]float64, thumb*thumb)
	cellX := make([]int, w)
	for x := 0; x < w; x++ {
		cellX[x] = x * thumb / w
	}
	switch m := img.(type) {
	case *image.YCbCr: // normal colour JPEG - read brightness channel directly (fast)
		for y := 0; y < h; y++ {
			row := (y * thumb / h) * thumb
			off := m.YOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				sums[row+cellX[x]] += float64(m.Y[off+x])
				counts[row+cellX[x]]++
			}
		}
	case *image.Gray: // black & white JPEG
		for y := 0; y < h; y++ {
			row := (y * thumb / h) * thumb
			off := m.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				sums[row+cellX[x]] += float64(m.Pix[off+x])
				counts[row+cellX[x]]++
			}
		}
	default: // rare formats (e.g. CMYK) - sample a grid
		step := max(1, max(w, h)/512)
		for y := 0; y < h; y += step {
			row := (y * thumb / h) * thumb
			for x := 0; x < w; x += step {
				r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				sums[row+cellX[x]] += (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257
				counts[row+cellX[x]]++
			}
		}
	}
	for i := range sums {
		if counts[i] > 0 {
			sums[i] /= counts[i]
		}
	}
	return sums
}

// orient rotates/flips the thumbnail to match the camera's EXIF orientation,
// so a photo and its "rotated" copy look the same to the comparison.
func orient(src []float64, o int) []float64 {
	if o <= 1 || o > 8 {
		return src
	}
	dst := make([]float64, len(src))
	n := thumb
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = n-1-x, y
			case 3:
				sx, sy = n-1-x, n-1-y
			case 4:
				sx, sy = x, n-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, n-1-x
			case 7:
				sx, sy = n-1-y, n-1-x
			case 8:
				sx, sy = n-1-y, x
			}
			dst[y*n+x] = src[sy*n+sx]
		}
	}
	return dst
}

var dctCos [thumb][thumb]float64

func init() {
	for u := 0; u < thumb; u++ {
		for x := 0; x < thumb; x++ {
			dctCos[u][x] = math.Cos(float64(2*x+1) * float64(u) * math.Pi / (2 * thumb))
		}
	}
}

func pHash(g []float64) uint64 {
	// 2-D DCT, keeping only the 8x8 lowest frequencies
	var tmp [8][thumb]float64
	for u := 0; u < 8; u++ {
		for y := 0; y < thumb; y++ {
			s := 0.0
			for x := 0; x < thumb; x++ {
				s += g[y*thumb+x] * dctCos[u][x]
			}
			tmp[u][y] = s
		}
	}
	coef := make([]float64, 0, 64)
	for v := 0; v < 8; v++ {
		for u := 0; u < 8; u++ {
			s := 0.0
			for y := 0; y < thumb; y++ {
				s += tmp[u][y] * dctCos[v][y]
			}
			coef = append(coef, s)
		}
	}
	sorted := append([]float64(nil), coef[1:]...) // skip overall brightness term
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	var h uint64
	for i, c := range coef {
		if i > 0 && c > median {
			h |= 1 << uint(i)
		}
	}
	return h
}

func dHash(g []float64) uint64 {
	// shrink 32x32 to 9x8 and compare neighbours
	var cell [8][9]float64
	for y := 0; y < 8; y++ {
		for x := 0; x < 9; x++ {
			x0, x1 := x*thumb/9, (x+1)*thumb/9
			y0, y1 := y*thumb/8, (y+1)*thumb/8
			s, c := 0.0, 0.0
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					s += g[yy*thumb+xx]
					c++
				}
			}
			cell[y][x] = s / c
		}
	}
	var h uint64
	bit := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if cell[y][x] > cell[y][x+1] {
				h |= 1 << uint(bit)
			}
			bit++
		}
	}
	return h
}

func hashDist(a, b *photo) (int, int) {
	return bits.OnesCount64(a.PHash ^ b.PHash), bits.OnesCount64(a.DHash ^ b.DHash)
}

func isLookAlike(a, b *photo, level sensitivity) bool {
	p, d := hashDist(a, b)
	return p <= level.PMax && d <= level.DMax && p+d <= level.SumMax
}

// similarityPct turns the fingerprint distance into a friendly percentage.
func similarityPct(a, b *photo) int {
	p, d := hashDist(a, b)
	return 100 - (p+d)*100/128
}

// exifOrientation reads the camera rotation flag (1-8) from JPEG data.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // image data starts - no EXIF found
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		seg := i + 4
		end := i + 2 + size
		if end > len(data) || size < 2 {
			return 1
		}
		if marker == 0xE1 && end-seg > 14 && string(data[seg:seg+6]) == "Exif\x00\x00" {
			t := data[seg+6 : end]
			var bo binary.ByteOrder
			switch string(t[:2]) {
			case "II":
				bo = binary.LittleEndian
			case "MM":
				bo = binary.BigEndian
			default:
				return 1
			}
			ifd := int(bo.Uint32(t[4:]))
			if ifd+2 > len(t) {
				return 1
			}
			n := int(bo.Uint16(t[ifd:]))
			for k := 0; k < n; k++ {
				e := ifd + 2 + k*12
				if e+12 > len(t) {
					return 1
				}
				if bo.Uint16(t[e:]) == 0x0112 {
					return int(bo.Uint16(t[e+8:]))
				}
			}
			return 1
		}
		i = end
	}
	return 1
}
