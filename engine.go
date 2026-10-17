package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type photo struct {
	ID      int
	Path    string
	Size    int64
	ModTime time.Time
	SHA     string
	PHash   uint64
	DHash   uint64
	HashOK  bool
	W, H    int // as displayed (camera rotation applied)
	Orient  int
	Sim     int // % similarity to the main photo of its look-alike set

	// face checks (look-alike sets only)
	Faces    []face
	Blinks   int  // faces with eyes closed compared with the other frames
	Soft     bool // faces less sharp than in the best frame
	Sharpest bool // best faces in the set
}

// A set is a group of files that are copies of one picture.
// Kind is "exact" (all byte-identical) or "similar" (look-alikes).
// Files[0] is the suggested keeper.
type dupSet struct {
	ID       int
	Kind     string
	Files    []*photo
	FacePick bool // the keeper was chosen by the face checks
}

type scanOptions struct {
	Level sensitivity
	Faces bool // check faces in look-alike sets
}

type progress struct {
	Phase string // finding, reading, comparing, faces
	Done  int64
	Total int64
}

type scanResult struct {
	Root       string
	Level      sensitivity
	Faces      bool
	Remembered int // photos not re-read because DDupe remembered them
	Photos     []*photo
	Sets       []*dupSet
	Unreadable int
	Took       time.Duration
}

func isPhotoFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".jpe", ".jfif":
		return true
	}
	return false
}

func scan(ctx context.Context, root string, opt scanOptions, prog *progress, mu *sync.Mutex) (*scanResult, error) {
	level := opt.Level
	cache := openCache()
	start := time.Now()
	set := func(phase string, done, total int64) {
		mu.Lock()
		prog.Phase, prog.Done, prog.Total = phase, done, total
		mu.Unlock()
	}

	// 1. Find photos
	set("finding", 0, 0)
	var photos []*photo
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && isSkippedDir(p, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isPhotoFile(p) {
			if info, err := d.Info(); err == nil && info.Size() > 0 {
				photos = append(photos, &photo{ID: len(photos), Path: p, Size: info.Size(), ModTime: info.ModTime()})
				if len(photos)%200 == 0 {
					set("finding", int64(len(photos)), 0)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 2. Read and fingerprint every photo, in parallel
	total := int64(len(photos))
	set("reading", 0, total)
	var done, remembered int64
	jobs := make(chan *photo)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ph := range jobs {
				if e := cache.get(ph); e != nil {
					ph.SHA, ph.PHash, ph.DHash, ph.HashOK = e.SHA, e.PHash, e.DHash, e.HashOK
					ph.W, ph.H, ph.Orient = e.W, e.H, e.Orient
					atomic.AddInt64(&remembered, 1)
				} else {
					analyze(ph)
					cache.put(ph, nil)
				}
				n := atomic.AddInt64(&done, 1)
				if n%8 == 0 || n == total {
					set("reading", n, total)
				}
			}
		}()
	}
feed:
	for i, ph := range photos {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- ph:
		}
		if i%500 == 0 {
			cache.saveIfDue()
		}
	}
	close(jobs)
	wg.Wait()
	cache.save() // keep what was learned, even if the scan was stopped
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	seen := make(map[string]bool, len(photos))
	for _, ph := range photos {
		seen[cacheKey(ph.Path)] = true
	}
	cache.forgetMissing(root, seen)

	// 3. Exact duplicates
	bySHA := map[string][]*photo{}
	unreadable := 0
	for _, ph := range photos {
		if ph.SHA != "" {
			bySHA[ph.SHA] = append(bySHA[ph.SHA], ph)
		}
		if !ph.HashOK {
			unreadable++
		}
	}
	for _, g := range bySHA {
		sortGroup(g)
	}

	// 4. Look-alikes (one representative per exact group)
	var reps []*photo
	seenSHA := map[string]bool{}
	for _, ph := range photos {
		if ph.HashOK && !seenSHA[ph.SHA] {
			seenSHA[ph.SHA] = true
			reps = append(reps, ph)
		}
	}
	similar, err := findSimilar(ctx, reps, bySHA, level, func(d, t int) { set("comparing", int64(d), int64(t)) })
	if err != nil {
		return nil, err
	}

	// 5. One combined list of sets; every file belongs to at most one set.
	var sets []*dupSet
	inSet := map[string]bool{}
	for _, g := range similar {
		sets = append(sets, &dupSet{Kind: "similar", Files: g})
		for _, ph := range g {
			inSet[ph.SHA] = true
		}
	}
	for sha, g := range bySHA {
		if len(g) > 1 && !inSet[sha] {
			sets = append(sets, &dupSet{Kind: "exact", Files: g})
		}
	}
	// 6. Face checks: blinks and focus, and the best frame to keep.
	if opt.Faces {
		if err := checkFaces(ctx, sets, cache, set); err != nil {
			cache.save()
			return nil, err
		}
	}
	cache.save()

	sort.Slice(sets, func(i, j int) bool { return sets[i].Files[0].Path < sets[j].Files[0].Path })
	for i, s := range sets {
		s.ID = i + 1
	}
	return &scanResult{Root: root, Level: level, Faces: opt.Faces, Remembered: int(remembered), Photos: photos, Sets: sets, Unreadable: unreadable, Took: time.Since(start)}, nil
}

// checkFaces looks at the faces in every look-alike set (only those photos,
// so even whole-drive scans stay quick), flags blinks and soft focus, and
// moves the best frame to the front of its set as the suggested keeper.
func checkFaces(ctx context.Context, sets []*dupSet, cache *fpCache, set func(string, int64, int64)) error {
	var todo []*photo
	known := map[string]faceInfo{} // by file content: exact copies share results
	for _, s := range sets {
		if s.Kind != "similar" {
			continue
		}
		for _, ph := range s.Files {
			if _, ok := known[ph.SHA]; ok {
				continue
			}
			if fi, ok := cache.facesFor(ph); ok {
				known[ph.SHA] = fi
				continue
			}
			known[ph.SHA] = faceInfo{}
			todo = append(todo, ph)
		}
	}
	total := int64(len(todo))
	set("faces", 0, total)
	var mu sync.Mutex
	var done int64
	jobs := make(chan *photo)
	var wg sync.WaitGroup
	for w := 0; w < max(1, runtime.NumCPU()-1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ph := range jobs {
				fi := analyzeFaces(ph)
				cache.put(ph, &fi)
				mu.Lock()
				known[ph.SHA] = fi
				mu.Unlock()
				set("faces", atomic.AddInt64(&done, 1), total)
			}
		}()
	}
feed:
	for i, ph := range todo {
		select {
		case <-ctx.Done():
			break feed
		case jobs <- ph:
		}
		if i%100 == 0 {
			cache.saveIfDue()
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, s := range sets {
		if s.Kind != "similar" {
			continue
		}
		infos := make([]faceInfo, len(s.Files))
		for i, ph := range s.Files {
			infos[i] = known[ph.SHA]
			ph.Faces = infos[i].Faces
		}
		verdicts, best := judgeSet(s, infos)
		for i, ph := range s.Files {
			ph.Blinks, ph.Soft, ph.Sharpest = verdicts[i].Blinks, verdicts[i].Soft, verdicts[i].Sharpest
			ph.Faces = verdicts[i].Confirmed
		}
		if best > 0 {
			keep := s.Files[best]
			rest := append([]*photo{keep}, s.Files[:best]...)
			s.Files = append(rest, s.Files[best+1:]...)
			s.FacePick = true
			// match % is measured against the keeper
			for _, ph := range s.Files {
				if ph.SHA == keep.SHA {
					ph.Sim = 100
				} else {
					ph.Sim = similarityPct(keep, ph)
				}
			}
		}
	}
	return nil
}

func analyze(ph *photo) {
	data, err := os.ReadFile(ph.Path)
	if err != nil {
		return
	}
	sum := sha256.Sum256(data)
	ph.SHA = hex.EncodeToString(sum[:])
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	b := img.Bounds()
	ph.Orient = exifOrientation(data)
	ph.W, ph.H = b.Dx(), b.Dy()
	if ph.Orient >= 5 && ph.Orient <= 8 {
		ph.W, ph.H = ph.H, ph.W
	}
	g := orient(grayThumb(img), ph.Orient)
	ph.PHash = pHash(g)
	ph.DHash = dHash(g)
	ph.HashOK = true
}

// findSimilar builds look-alike sets. Photos are taken largest-first; each
// unclaimed photo starts a set and collects every unclaimed photo that is
// close to IT (not merely close to some other member), so unrelated photos
// can't be chained together into one set.
func findSimilar(ctx context.Context, reps []*photo, bySHA map[string][]*photo, level sensitivity, report func(done, total int)) ([][]*photo, error) {
	sort.SliceStable(reps, func(i, j int) bool {
		ai, aj := reps[i].W*reps[i].H, reps[j].W*reps[j].H
		if ai != aj {
			return ai > aj
		}
		return reps[i].Size > reps[j].Size
	})
	n := len(reps)
	claimed := make([]bool, n)
	var out [][]*photo
	workers := runtime.NumCPU()
	matches := make([]bool, n)
	for i := 0; i < n; i++ {
		if i%64 == 0 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			report(i, n)
		}
		if claimed[i] {
			continue
		}
		center := reps[i]
		var wg sync.WaitGroup
		chunk := (n - i + workers - 1) / workers
		for w := 0; w < workers; w++ {
			lo, hi := i+1+w*chunk, min(n, i+1+(w+1)*chunk)
			if lo >= hi {
				continue
			}
			wg.Add(1)
			go func(lo, hi int) {
				defer wg.Done()
				for j := lo; j < hi; j++ {
					matches[j] = !claimed[j] && isLookAlike(center, reps[j], level)
				}
			}(lo, hi)
		}
		wg.Wait()
		group := []*photo{center}
		for j := i + 1; j < n; j++ {
			if matches[j] {
				claimed[j] = true
				group = append(group, reps[j])
			}
		}
		if len(group) < 2 {
			continue
		}
		claimed[i] = true
		var all []*photo
		for _, rep := range group {
			sim := similarityPct(center, rep)
			for _, ph := range bySHA[rep.SHA] {
				ph.Sim = sim
				all = append(all, ph)
			}
		}
		out = append(out, all)
	}
	report(n, n)
	return out, nil
}

// Oldest file first (most likely the "original"), then shortest path.
func sortGroup(g []*photo) {
	sort.Slice(g, func(i, j int) bool {
		if !g[i].ModTime.Equal(g[j].ModTime) {
			return g[i].ModTime.Before(g[j].ModTime)
		}
		if len(g[i].Path) != len(g[j].Path) {
			return len(g[i].Path) < len(g[j].Path)
		}
		return g[i].Path < g[j].Path
	})
}

type moveOutcome struct {
	Moved    int          `json:"moved"`
	Bytes    int64        `json:"bytes"`
	Failed   []string     `json:"failed"`
	DupesDir string       `json:"dupesDir"`
	Sheet    string       `json:"sheet"` // Excel file listing every move
	Records  []moveRecord `json:"-"`
}

type moveRecord struct {
	Set    *dupSet
	Photo  *photo
	From   string // original full path
	To     string // new full path inside DUPES ("" if it couldn't be moved)
	Kept   []string
	Reason string
}

// moveFiles moves the chosen files into ROOT\DUPES, keeping subfolders,
// and logs each move together with the file(s) kept from its set.
func moveFiles(res *scanResult, move map[int]bool, report func(done, total int)) moveOutcome {
	dupRoot := filepath.Join(res.Root, "DUPES")
	out := moveOutcome{DupesDir: dupRoot}
	os.MkdirAll(dupRoot, 0755)
	mlog, _ := os.OpenFile(filepath.Join(dupRoot, "moved_files_log.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if mlog != nil {
		defer mlog.Close()
		mlog.WriteString("--- " + time.Now().Format("2006-01-02 15:04") + " ---\r\n")
	}
	total, done := len(move), 0
	for _, s := range res.Sets {
		var kept []string
		for _, ph := range s.Files {
			if !move[ph.ID] {
				kept = append(kept, ph.Path)
			}
		}
		for _, ph := range s.Files {
			if !move[ph.ID] {
				continue
			}
			done++
			report(done, total)
			rel, err := filepath.Rel(res.Root, ph.Path)
			reason := "Look-alike"
			if s.Kind == "exact" || (len(s.Files) > 0 && ph.SHA == s.Files[0].SHA) {
				reason = "Exact duplicate"
			}
			rec := moveRecord{Set: s, Photo: ph, From: ph.Path, Kept: kept, Reason: reason}
			if err != nil || strings.HasPrefix(rel, "..") {
				out.Failed = append(out.Failed, ph.Path)
				out.Records = append(out.Records, rec)
				continue
			}
			dest := filepath.Join(dupRoot, rel)
			os.MkdirAll(filepath.Dir(dest), 0755)
			if _, err := os.Stat(dest); err == nil {
				ext := filepath.Ext(dest)
				dest = strings.TrimSuffix(dest, ext) + "_" + time.Now().Format("150405.000000000") + ext
			}
			if err := os.Rename(ph.Path, dest); err != nil {
				logf("Could not move %s: %v", ph.Path, err)
				out.Failed = append(out.Failed, ph.Path)
				out.Records = append(out.Records, rec)
				continue
			}
			rec.To = dest
			out.Records = append(out.Records, rec)
			out.Moved++
			out.Bytes += ph.Size
			if mlog != nil {
				mlog.WriteString("[" + reason + "] " + ph.Path + "  ->  " + dest + "   (kept: " + strings.Join(kept, " | ") + ")\r\n")
			}
		}
	}
	return out
}
