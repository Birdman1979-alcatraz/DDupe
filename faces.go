package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/jpeg"
	"math"
	"os"
	"sort"
	"sync"

	"photodupfinder/pigo"
)

// Face checks for look-alike sets.
//
// Faces are found with pigo (MIT licence, see pigo/LICENSE), a small face and
// pupil finder that runs entirely on this computer. For each face DDupe
// measures:
//   - Sharpness: how crisp the face is (variance of the Laplacian, divided by
//     the face's contrast so brightness changes don't count as sharpness).
//   - Eye openness: how much detail there is around each pupil. An open eye
//     has a dark iris, bright white and sharp edges; a closed eye is mostly
//     smooth skin with a thin lash line.
// These numbers mean little on their own, but they are very reliable when the
// same face is compared across near-identical frames of one look-alike set,
// which is exactly how DDupe uses them.

//go:embed cascades/facefinder
var facefinderModel []byte

//go:embed cascades/puploc
var puplocModel []byte

var (
	faceOnce  sync.Once
	faceDet   *pigo.Pigo
	pupilDet  *pigo.PuplocCascade
	faceInitE error
)

func initFaces() error {
	faceOnce.Do(func() {
		faceDet, faceInitE = pigo.NewPigo().Unpack(facefinderModel)
		if faceInitE == nil {
			pupilDet, faceInitE = pigo.NewPuplocCascade().UnpackCascade(puplocModel)
		}
	})
	return faceInitE
}

type face struct {
	X, Y, Size float64      // centre and size, as fractions of image width
	Sharp      float64      // face sharpness
	Eyes       float64      // eye openness (average of both eyes)
	EyePos     [4]float64   // left x,y and right x,y, as fractions of image width
	EyePatch   [2][]float32 // small normalised copy of each eye, to tell a blink from an edit
}

type faceInfo struct {
	Done  bool
	Faces []face
}

const faceWork = 1600

var eyeK = 1.2 // how much darker than the face an iris must be // analysis resolution (longest side)

// analyzeFaces finds faces in one photo and measures them.
func analyzeFaces(ph *photo) faceInfo {
	info := faceInfo{Done: true}
	if initFaces() != nil {
		return info
	}
	data, err := os.ReadFile(ph.Path)
	if err != nil {
		return info
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return info
	}
	gray, W, H := grayWorkCopy(img, faceWork, ph.Orient)

	// Detect on a half-size copy: faster, and plenty for finding faces.
	sw, sh := W/2, H/2
	small := make([]uint8, sw*sh)
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			i := 2*y*W + 2*x
			small[y*sw+x] = uint8((int(gray[i]) + int(gray[i+1]) + int(gray[i+W]) + int(gray[i+W+1])) / 4)
		}
	}
	params := pigo.ImageParams{Pixels: small, Rows: sh, Cols: sw, Dim: sw}
	dets := faceDet.RunCascade(pigo.CascadeParams{
		MinSize: max(24, min(sw, sh)/25), MaxSize: min(sw, sh), ShiftFactor: 0.1, ScaleFactor: 1.1, ImageParams: params,
	}, 0)
	dets = faceDet.ClusterDetections(dets, 0.2)
	sort.Slice(dets, func(i, j int) bool { return dets[i].Scale > dets[j].Scale })

	var kept []pigo.Detection
next:
	for _, d := range dets {
		if d.Q < 6 || len(info.Faces) >= 8 {
			continue
		}
		// the same face is sometimes found twice at different sizes: keep the larger
		for _, k := range kept {
			if math.Hypot(float64(d.Col-k.Col), float64(d.Row-k.Row)) < float64(k.Scale)*0.6 {
				continue next
			}
		}
		kept = append(kept, d)
		f := face{
			X: float64(d.Col) * 2 / float64(W), Y: float64(d.Row) * 2 / float64(W), Size: float64(d.Scale) * 2 / float64(W),
		}
		cx, cy, s := d.Col*2, d.Row*2, d.Scale*2
		std := regionStd(gray, W, H, cx-s/2, cy-s/2, s, s)
		f.Sharp = laplacianScore(gray, W, H, cx-s/2, cy-s/2, s, std)

		var eyes []float64
		for k, side := range []float64{-0.175, 0.185} {
			pl := pigo.Puploc{Row: d.Row - int(0.075*float32(d.Scale)), Col: d.Col + int(side*float64(d.Scale)), Scale: float32(d.Scale) * 0.25, Perturbs: 63}
			ex, ey := pl.Col, pl.Row
			if p := pupilDet.RunDetector(pl, params, 0, false); p != nil && p.Row > 0 && p.Col > 0 {
				// keep the pupil finder's answer only if it stays near where an eye should be
				if abs(p.Col-ex) < d.Scale/8 && abs(p.Row-ey) < d.Scale/8 {
					ex, ey = p.Col, p.Row
				}
			}
			f.EyePos[k*2], f.EyePos[k*2+1] = float64(ex)*2/float64(W), float64(ey)*2/float64(W)
			eyes = append(eyes, eyeAperture(gray, W, H, ex*2, ey*2, s, std))
			f.EyePatch[k] = eyePatch(gray, W, H, ex*2, ey*2, s)
		}
		f.Eyes = (eyes[0] + eyes[1]) / 2
		if s < 90 { // face too small to judge eyes reliably
			f.Eyes = -1
		}
		info.Faces = append(info.Faces, f)
	}
	return info
}

// eyeAperture measures how open an eye is: the height of its dark part.
// An open eye shows a dark iris and pupil several pixels tall; a closed eye
// shows only a thin lash line. In the area around the eye, pixels clearly
// darker than the surrounding skin are marked, and for each column the
// longest unbroken dark run is measured. The result is the typical tallest
// run, as a share of the face size, so faces of any size compare fairly.
func eyeAperture(g []uint8, W, H, ex, ey, faceSize int, faceStd float64) float64 {
	hw, hh := max(4, faceSize*9/100), max(3, faceSize*5/100)
	x0, y0, w, h := clampRect(W, H, ex-hw, ey-hh, 2*hw, 2*hh)
	if w < 4 || h < 4 {
		return 0
	}
	var sum, sum2 float64
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			v := float64(g[y*W+x])
			sum += v
			sum2 += v * v
		}
	}
	n := float64(w * h)
	mean := sum / n
	sd := math.Sqrt(math.Max(0, sum2/n-mean*mean))
	cut := math.Min(mean-0.75*sd, mean-eyeK*faceStd)
	runs := make([]int, 0, w)
	for x := x0; x < x0+w; x++ {
		best, cur := 0, 0
		for y := y0; y < y0+h; y++ {
			if float64(g[y*W+x]) < cut {
				cur++
				best = max(best, cur)
			} else {
				cur = 0
			}
		}
		runs = append(runs, best)
	}
	sort.Ints(runs)
	return float64(runs[len(runs)*85/100]) / float64(faceSize) * 100
}

// eyePatch keeps a 24x12 copy of the eye area with brightness and contrast
// normalised, so a brightened or recompressed copy still matches the
// original, while a blink (a different eye shape) does not.
func eyePatch(g []uint8, W, H, ex, ey, faceSize int) []float32 {
	const pw, ph = 24, 12
	hw, hh := max(4, faceSize*9/100), max(3, faceSize*5/100)
	x0, y0, w, h := clampRect(W, H, ex-hw, ey-hh, 2*hw, 2*hh)
	if w < 4 || h < 4 {
		return nil
	}
	p := make([]float32, pw*ph)
	var sum float64
	for j := 0; j < ph; j++ {
		for i := 0; i < pw; i++ {
			v := float64(g[(y0+j*h/ph)*W+x0+i*w/pw])
			p[j*pw+i] = float32(v)
			sum += v
		}
	}
	mean := sum / float64(len(p))
	var ss float64
	for i := range p {
		p[i] -= float32(mean)
		ss += float64(p[i]) * float64(p[i])
	}
	sd := float32(math.Sqrt(ss/float64(len(p))) + 1e-6)
	for i := range p {
		p[i] /= sd
	}
	return p
}

// eyesMatch reports how alike two faces' eyes look (1 = identical shape).
// It allows for the eye position being found a pixel or two apart.
func eyesMatch(a, b face) float64 {
	const pw, ph = 24, 12
	tot, n := 0.0, 0
	for k := 0; k < 2; k++ {
		pa, pb := a.EyePatch[k], b.EyePatch[k]
		if len(pa) != pw*ph || len(pb) != pw*ph {
			continue
		}
		best := -1.0
		for dy := -2; dy <= 2; dy++ {
			for dx := -3; dx <= 3; dx++ {
				var c float64
				m := 0
				for j := 2; j < ph-2; j++ {
					for i := 3; i < pw-3; i++ {
						c += float64(pa[j*pw+i]) * float64(pb[(j+dy)*pw+i+dx])
						m++
					}
				}
				best = math.Max(best, c/float64(m))
			}
		}
		tot += best
		n++
	}
	if n == 0 {
		return 1
	}
	return tot / float64(n)
}

func regionMean(g []uint8, W, H, x, y, w, h int) float64 {
	x, y, w, h = clampRect(W, H, x, y, w, h)
	if w < 1 || h < 1 {
		return 255
	}
	var s float64
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			s += float64(g[yy*W+xx])
		}
	}
	return s / float64(w*h)
}

// grayWorkCopy makes an upright grayscale copy, at most maxSide pixels on its
// longest side. JPEGs store brightness (Y) separately from colour, so it is
// read directly and averaged down, which is far faster than converting colour.
func grayWorkCopy(img image.Image, maxSide, orientation int) ([]uint8, int, int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	f := 1
	for max(w, h)/f > maxSide {
		f++
	}
	gw, gh := w/f, h/f
	g := make([]uint8, gw*gh)
	switch m := img.(type) {
	case *image.YCbCr:
		for y := 0; y < gh; y++ {
			for x := 0; x < gw; x++ {
				sum := 0
				for yy := 0; yy < f; yy++ {
					o := m.YOffset(b.Min.X+x*f, b.Min.Y+y*f+yy)
					for xx := 0; xx < f; xx++ {
						sum += int(m.Y[o+xx])
					}
				}
				g[y*gw+x] = uint8(sum / (f * f))
			}
		}
	case *image.Gray:
		for y := 0; y < gh; y++ {
			for x := 0; x < gw; x++ {
				sum := 0
				for yy := 0; yy < f; yy++ {
					o := m.PixOffset(b.Min.X+x*f, b.Min.Y+y*f+yy)
					for xx := 0; xx < f; xx++ {
						sum += int(m.Pix[o+xx])
					}
				}
				g[y*gw+x] = uint8(sum / (f * f))
			}
		}
	default:
		for y := 0; y < gh; y++ {
			for x := 0; x < gw; x++ {
				r, gg, bb, _ := img.At(b.Min.X+x*f, b.Min.Y+y*f).RGBA()
				g[y*gw+x] = uint8((299*(r>>8) + 587*(gg>>8) + 114*(bb>>8)) / 1000)
			}
		}
	}
	if orientation <= 1 || orientation > 8 {
		return g, gw, gh
	}
	dw, dh := gw, gh
	if orientation >= 5 {
		dw, dh = gh, gw
	}
	d := make([]uint8, dw*dh)
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = gw-1-x, y
			case 3:
				sx, sy = gw-1-x, gh-1-y
			case 4:
				sx, sy = x, gh-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, gh-1-x
			case 7:
				sx, sy = gw-1-y, gh-1-x
			case 8:
				sx, sy = gw-1-y, x
			}
			d[y*dw+x] = g[sy*gw+sx]
		}
	}
	return d, dw, dh
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clampRect(W, H, x, y, w, h int) (int, int, int, int) {
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x+w > W {
		w = W - x
	}
	if y+h > H {
		h = H - y
	}
	return x, y, w, h
}

func regionStd(g []uint8, W, H, x, y, w, h int) float64 {
	x, y, w, h = clampRect(W, H, x, y, w, h)
	if w < 2 || h < 2 {
		return 0
	}
	var s, s2 float64
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			v := float64(g[yy*W+xx])
			s += v
			s2 += v * v
		}
	}
	n := float64(w * h)
	return math.Sqrt(math.Max(0, s2/n-(s/n)*(s/n)))
}

// laplacianScore measures face sharpness on a fixed-size copy of the face
// (192x192), so faces of different sizes are compared fairly.
func laplacianScore(g []uint8, W, H, x, y, s int, std float64) float64 {
	const N = 192
	x, y, w, h := clampRect(W, H, x, y, s, s)
	if w < 16 || h < 16 {
		return 0
	}
	c := make([]float64, N*N)
	for j := 0; j < N; j++ {
		for i := 0; i < N; i++ {
			c[j*N+i] = float64(g[(y+j*h/N)*W+x+i*w/N])
		}
	}
	var sum, sum2 float64
	n := 0
	for j := 1; j < N-1; j++ {
		for i := 1; i < N-1; i++ {
			l := 4*c[j*N+i] - c[j*N+i-1] - c[j*N+i+1] - c[(j-1)*N+i] - c[(j+1)*N+i]
			sum += l
			sum2 += l * l
			n++
		}
	}
	v := sum2/float64(n) - (sum/float64(n))*(sum/float64(n))
	return v / (std*std + 1) * 100
}

// ----- comparing frames inside a look-alike set -----

type frameVerdict struct {
	Confirmed []face // faces that also appear in the other frames
	Faces     int
	Blinks    int  // faces whose eyes look closed compared with other frames
	Soft      bool // faces noticeably less sharp than the best frame
	Sharpest  bool
	Score     float64
}

// judgeSet compares faces across the frames of one look-alike set and picks
// the best frame to keep. infos is indexed like s.Files. It returns a verdict
// per file and the index of the suggested keeper (-1 to keep the default).
func judgeSet(s *dupSet, infos []faceInfo) ([]frameVerdict, int) {
	n := len(s.Files)
	out := make([]frameVerdict, n)
	maxPix := 0
	for _, ph := range s.Files {
		maxPix = max(maxPix, ph.W*ph.H)
	}
	anyFaces := false
	for i := range infos {
		out[i].Faces = len(infos[i].Faces)
		if out[i].Faces > 0 {
			anyFaces = true
		}
	}
	if !anyFaces {
		return out, -1
	}

	// Match faces across frames by position: the frames are near-identical,
	// so the same person sits in about the same place.
	type track struct {
		eyes, sharp []float64
		f           []face
	}
	var ref []face
	for _, inf := range infos {
		if len(inf.Faces) > len(ref) {
			ref = inf.Faces
		}
	}
	tracks := make([]track, len(ref))
	for t := range tracks {
		tracks[t] = track{eyes: make([]float64, n), sharp: make([]float64, n), f: make([]face, n)}
		for i := 0; i < n; i++ {
			tracks[t].eyes[i], tracks[t].sharp[i] = -1, -1
		}
	}
	for i, inf := range infos {
		for _, f := range inf.Faces {
			best, bd := -1, math.MaxFloat64
			for t, r := range ref {
				d := math.Hypot(f.X-r.X, f.Y-r.Y)
				if d < r.Size*0.6 && d < bd {
					best, bd = t, d
				}
			}
			if best >= 0 {
				tracks[best].eyes[i], tracks[best].sharp[i], tracks[best].f[i] = f.Eyes, f.Sharp, f
			}
		}
	}

	// Only full-size frames compete; smaller copies are exports of them.
	full := make([]bool, n)
	for i, ph := range s.Files {
		full[i] = ph.W*ph.H*3 >= maxPix*2
	}
	nFull := 0
	for i := range full {
		if full[i] {
			nFull++
		}
	}
	anySoft := false
	for _, tr := range tracks {
		// A real person appears in most frames; a face found in only one or
		// two frames is a false detection (a pattern in the background).
		seenIn := 0
		for i := 0; i < n; i++ {
			if full[i] && tr.sharp[i] >= 0 {
				seenIn++
			}
		}
		if seenIn*2 <= nFull && !(nFull == 1 && seenIn == 1) {
			continue
		}
		for i := 0; i < n; i++ {
			if tr.sharp[i] >= 0 {
				out[i].Confirmed = append(out[i].Confirmed, tr.f[i])
			}
		}
		maxEye, maxSharp, open := 0.0, 0.0, -1
		for i := 0; i < n; i++ {
			if full[i] {
				if tr.eyes[i] > maxEye {
					maxEye, open = tr.eyes[i], i
				}
				maxSharp = math.Max(maxSharp, tr.sharp[i])
			}
		}
		for i := 0; i < n; i++ {
			if !full[i] || tr.sharp[i] < 0 {
				continue
			}
			// A blink: clearly less open than the most open frame AND the eyes
			// really look different (an edited copy has the same eye shape).
			if tr.eyes[i] >= 0 && maxEye >= 1.0 && tr.eyes[i] < maxEye*0.62 &&
				open >= 0 && eyesMatch(tr.f[i], tr.f[open]) < 0.8 {
				out[i].Blinks++
			}
			if maxSharp > 0 && tr.sharp[i] < maxSharp*0.55 {
				out[i].Soft = true
				anySoft = true
			}
			out[i].Score += tr.sharp[i] / math.Max(maxSharp, 1e-9)
		}
	}

	// Best frame: full size, fewest blinks, then sharpest faces.
	best := -1
	for i := 0; i < n; i++ {
		if !full[i] || out[i].Faces == 0 {
			continue
		}
		if best < 0 || out[i].Blinks < out[best].Blinks ||
			(out[i].Blinks == out[best].Blinks && out[i].Score > out[best].Score*1.03) {
			best = i
		}
	}
	if best >= 0 {
		if anySoft {
			out[best].Sharpest = true
		}
		// Only replace the usual keeper when its faces give a real reason:
		// someone blinked, or the faces are soft.
		def := 0
		if out[def].Blinks <= out[best].Blinks && !out[def].Soft {
			best = -1
		}
	}
	return out, best
}
