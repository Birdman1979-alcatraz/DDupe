package main

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DDupe remembers what it has learned about each photo (its fingerprints and
// face checks) in %LOCALAPPDATA%\DDupe\fingerprints.bin. On the next scan,
// photos whose size and date haven't changed are not read again, so
// rescanning a big drive takes minutes instead of hours, and a scan that was
// stopped picks up where it left off.

const cacheVersion = 2

type cacheEntry struct {
	Size   int64
	Mod    int64
	SHA    string
	PHash  uint64
	DHash  uint64
	HashOK bool
	W, H   int
	Orient int
	Faces  faceInfo
}

type fpCache struct {
	mu      sync.Mutex
	path    string
	entries map[string]*cacheEntry
	dirty   int
	saved   time.Time
}

type cacheFile struct {
	Version int
	Entries map[string]*cacheEntry
}

func openCache() *fpCache {
	c := &fpCache{path: filepath.Join(filepath.Dir(logPath()), "fingerprints.bin"), entries: map[string]*cacheEntry{}, saved: time.Now()}
	f, err := os.Open(c.path)
	if err != nil {
		return c
	}
	defer f.Close()
	var cf cacheFile
	if gob.NewDecoder(f).Decode(&cf) == nil && cf.Version == cacheVersion && cf.Entries != nil {
		c.entries = cf.Entries
	}
	return c
}

func cacheKey(p string) string { return strings.ToLower(p) }

// get returns what's remembered about this exact file, if it hasn't changed.
func (c *fpCache) get(ph *photo) *cacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[cacheKey(ph.Path)]
	if e == nil || e.Size != ph.Size || e.Mod != ph.ModTime.UnixNano() {
		return nil
	}
	return e
}

func (c *fpCache) put(ph *photo, faces *faceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := cacheKey(ph.Path)
	e := c.entries[k]
	if e == nil || e.Size != ph.Size || e.Mod != ph.ModTime.UnixNano() {
		e = &cacheEntry{}
		c.entries[k] = e
	}
	e.Size, e.Mod, e.SHA, e.PHash, e.DHash, e.HashOK = ph.Size, ph.ModTime.UnixNano(), ph.SHA, ph.PHash, ph.DHash, ph.HashOK
	e.W, e.H, e.Orient = ph.W, ph.H, ph.Orient
	if faces != nil {
		e.Faces = *faces
	}
	c.dirty++
}

func (c *fpCache) facesFor(ph *photo) (faceInfo, bool) {
	if e := c.get(ph); e != nil && e.Faces.Done {
		return e.Faces, true
	}
	return faceInfo{}, false
}

// forgetMissing drops remembered photos under root that no longer exist
// there (deleted, or moved into DUPES).
func (c *fpCache) forgetMissing(root string, seen map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := cacheKey(strings.TrimRight(root, `\/`) + string(filepath.Separator))
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) && !seen[k] {
			delete(c.entries, k)
			c.dirty++
		}
	}
}

// saveIfDue saves every half minute during a long scan, so stopping (or the
// program being closed) loses little work.
func (c *fpCache) saveIfDue() {
	c.mu.Lock()
	due := c.dirty > 0 && time.Since(c.saved) > 30*time.Second
	c.mu.Unlock()
	if due {
		c.save()
	}
}

func (c *fpCache) save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty == 0 {
		return
	}
	os.MkdirAll(filepath.Dir(c.path), 0755)
	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	err = gob.NewEncoder(f).Encode(cacheFile{Version: cacheVersion, Entries: c.entries})
	f.Close()
	if err == nil && os.Rename(tmp, c.path) == nil {
		c.dirty = 0
		c.saved = time.Now()
	} else {
		os.Remove(tmp)
	}
}

// ----- folders DDupe never needs to look in -----

// Skipped anywhere: DDupe's own folder, Windows' per-user app data and the
// Recycle Bin. Skipped only at the top of a drive: Windows' own folders.
var skipAnywhere = []string{"dupes", "_duplicates", "appdata", "$recycle.bin", "system volume information"}
var skipAtDriveTop = []string{"windows", "program files", "program files (x86)", "programdata", "perflogs",
	"recovery", "$winreagent", "$windows.~bt", "$windows.~ws", "msocache", "config.msi", "intel", "amd", "nvidia"}

func isSkippedDir(path, name string) bool {
	n := strings.ToLower(name)
	if strings.HasSuffix(n, ".lrdata") { // Lightroom's preview cache
		return true
	}
	for _, s := range skipAnywhere {
		if n == s {
			return true
		}
	}
	parent := filepath.Dir(path)
	if parent == filepath.VolumeName(path)+string(filepath.Separator) || parent == "/" {
		for _, s := range skipAtDriveTop {
			if n == s {
				return true
			}
		}
	}
	return false
}
