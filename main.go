// DDupe - duplicate photo finder for Windows.
//
// DDupe runs a small web server that only this computer can reach
// (127.0.0.1, with a random port and a secret token) and shows its interface
// in a Microsoft Edge app window. Closing the window closes DDupe.
// Photos are never deleted or changed; duplicates are only moved into a
// DUPES folder, after the user confirms.
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const version = "2.4.1"

//go:embed ui
var uiFiles embed.FS

type app struct {
	token string

	mu       sync.Mutex
	phase    string // idle, scanning, results, moving, done
	prog     progress
	err      string
	res      *scanResult
	cancel   context.CancelFunc
	outcome  *moveOutcome
	busyMove atomic.Bool
	gen      int // counts scans, so previews from an earlier scan are never reused

	conns    atomic.Int64
	lastSeen atomic.Int64 // unix nanos of last time a window was connected
	everConn atomic.Bool
}

func main() {
	startLog()
	a := &app{phase: "idle"}
	tok := make([]byte, 16)
	rand.Read(tok)
	a.token = hex.EncodeToString(tok)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		showError("DDupe could not start: " + err.Error())
		return
	}
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), a.token)

	mux := http.NewServeMux()
	ui, _ := fs.Sub(uiFiles, "ui")
	mux.Handle("/", http.FileServer(http.FS(ui)))
	mux.HandleFunc("/api/alive", a.guard(a.handleAlive))
	mux.HandleFunc("/api/state", a.guard(a.handleState))
	mux.HandleFunc("/api/recent", a.guard(a.handleRecent))
	mux.HandleFunc("/api/pick", a.guard(a.handlePick))
	mux.HandleFunc("/api/scan", a.guard(a.handleScan))
	mux.HandleFunc("/api/cancel", a.guard(a.handleCancel))
	mux.HandleFunc("/api/results", a.guard(a.handleResults))
	mux.HandleFunc("/api/move", a.guard(a.handleMove))
	mux.HandleFunc("/api/report", a.guard(a.handleReport))
	mux.HandleFunc("/api/open", a.guard(a.handleOpen))
	mux.HandleFunc("/api/reset", a.guard(a.handleReset))
	mux.HandleFunc("/thumb/", a.guard(a.handleThumb))
	mux.HandleFunc("/img/", a.guard(a.handleImage))

	go http.Serve(ln, mux)
	go a.watchdog()

	if os.Getenv("DDUPE_NO_WINDOW") != "" {
		fmt.Println(url)
	} else if !openAppWindow(url) {
		showError("DDupe could not open its window.\n\nOpen this address in your web browser instead:\n" + url)
	}
	select {}
}

// watchdog closes DDupe once its window has been closed for a few seconds.
func (a *app) watchdog() {
	start := time.Now()
	for range time.Tick(time.Second) {
		if a.busyMove.Load() {
			continue
		}
		if !a.everConn.Load() {
			if time.Since(start) > 3*time.Minute && os.Getenv("DDUPE_NO_WINDOW") == "" {
				os.Exit(0)
			}
			continue
		}
		if a.conns.Load() == 0 && time.Since(time.Unix(0, a.lastSeen.Load())) > 8*time.Second {
			os.Exit(0)
		}
	}
}

// guard rejects requests that don't carry the secret token, so other programs
// and web pages can't use DDupe's controls.
func (a *app) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := r.Header.Get("X-DDupe-Token")
		if t == "" {
			t = r.URL.Query().Get("t")
		}
		if t != a.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-DDupe-Token") != a.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20)).Decode(v)
}

// handleAlive keeps an open connection while the window exists.
func (a *app) handleAlive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	a.conns.Add(1)
	a.everConn.Store(true)
	defer func() {
		a.conns.Add(-1)
		a.lastSeen.Store(time.Now().UnixNano())
	}()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		fmt.Fprint(w, ": ok\n\n")
		if fl != nil {
			fl.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
		}
	}
}

func (a *app) handleState(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]any{
		"phase": a.phase, "error": a.err, "version": version, "log": logPath(),
		"progress": map[string]any{"phase": a.prog.Phase, "done": a.prog.Done, "total": a.prog.Total},
	}
	if a.res != nil {
		out["root"] = a.res.Root
	}
	if a.outcome != nil {
		out["outcome"] = a.outcome
	}
	writeJSON(w, out)
}

// ----- recent folders -----

func recentPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "DDupe", "recent.json")
}

func loadRecent() []string {
	var list []string
	if b, err := os.ReadFile(recentPath()); err == nil {
		json.Unmarshal(b, &list)
	}
	var out []string
	for _, p := range list {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func addRecent(p string) {
	list := []string{p}
	for _, q := range loadRecent() {
		if !strings.EqualFold(q, p) && len(list) < 6 {
			list = append(list, q)
		}
	}
	if rp := recentPath(); rp != "" {
		os.MkdirAll(filepath.Dir(rp), 0755)
		b, _ := json.Marshal(list)
		os.WriteFile(rp, b, 0644)
	}
}

func (a *app) handleRecent(w http.ResponseWriter, r *http.Request) {
	list := loadRecent()
	if list == nil {
		list = []string{}
	}
	writeJSON(w, list)
}

func (a *app) handlePick(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"path": pickFolder()})
}

// ----- scanning -----

func (a *app) handleScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Level int    `json:"level"`
		Faces bool   `json:"faces"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	root := strings.Trim(strings.TrimSpace(req.Path), `"`)
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		writeJSON(w, map[string]string{"error": "That folder can't be found. Check the address, or use Choose folder."})
		return
	}
	root, _ = filepath.Abs(root)
	if req.Level < 0 || req.Level >= len(levels) {
		req.Level = 1
	}
	a.mu.Lock()
	if a.phase == "scanning" || a.phase == "moving" {
		a.mu.Unlock()
		writeJSON(w, map[string]string{"error": "DDupe is busy. Wait for the current task to finish."})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.phase, a.err, a.res, a.outcome, a.cancel = "scanning", "", nil, nil, cancel
	a.prog = progress{Phase: "finding"}
	a.mu.Unlock()
	addRecent(root)

	logf("Scan started: %s (%s matching, face checks %v)", root, levels[req.Level].Name, req.Faces)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("Scan crashed: %v", r)
				a.mu.Lock()
				a.phase, a.err = "idle", fmt.Sprintf("The scan stopped because of an error: %v", r)
				a.mu.Unlock()
			}
		}()
		res, err := scan(ctx, root, scanOptions{Level: levels[req.Level], Faces: req.Faces}, &a.prog, &a.mu)
		if err == nil {
			logf("Scan finished: %d photos (%d remembered), %d sets, %d unreadable, %s", len(res.Photos), res.Remembered, len(res.Sets), res.Unreadable, res.Took.Round(time.Second))
		} else {
			logf("Scan ended: %v", err)
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		switch {
		case errors.Is(err, context.Canceled):
			a.phase = "idle"
		case err != nil:
			a.phase, a.err = "idle", "The scan stopped: "+err.Error()
		default:
			if res.Root == "" {
				res.Root = root
			}
			a.gen++
			a.res, a.phase = res, "results"
		}
	}()
	writeJSON(w, map[string]string{"ok": "1"})
}

func (a *app) handleCancel(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.cancel != nil && a.phase == "scanning" {
		a.cancel()
	}
	a.mu.Unlock()
	writeJSON(w, map[string]string{"ok": "1"})
}

func (a *app) handleReset(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	if a.phase != "scanning" && a.phase != "moving" {
		a.phase, a.res, a.outcome, a.err = "idle", nil, nil, ""
	}
	a.mu.Unlock()
	writeJSON(w, map[string]string{"ok": "1"})
}

type fileJSON struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Size   int64  `json:"size"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	Date   string `json:"date"`
	Sim    int    `json:"sim"`
	Same   bool   `json:"same"` // byte-identical to the set's main photo

	Faces    []faceJSON `json:"faces,omitempty"`
	Blinks   int        `json:"blinks,omitempty"`
	Soft     bool       `json:"soft,omitempty"`
	Sharpest bool       `json:"sharpest,omitempty"`
}

// faceJSON: a face's box and eyes, as fractions of the photo's width.
type faceJSON struct {
	X, Y, S float64
	Eyes    [4]float64
}

type setJSON struct {
	ID       int        `json:"id"`
	Kind     string     `json:"kind"`
	Files    []fileJSON `json:"files"`
	FacePick bool       `json:"facePick,omitempty"`
}

func (a *app) handleResults(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	res := a.res
	a.mu.Unlock()
	if res == nil {
		writeJSON(w, map[string]any{"error": "No results yet."})
		return
	}
	sets := make([]setJSON, 0, len(res.Sets))
	for _, s := range res.Sets {
		sj := setJSON{ID: s.ID, Kind: s.Kind, FacePick: s.FacePick}
		for i, ph := range s.Files {
			rel, err := filepath.Rel(res.Root, filepath.Dir(ph.Path))
			if err != nil || rel == "." {
				rel = ""
			}
			sim := 100
			if s.Kind == "similar" && i > 0 && ph.SHA != s.Files[0].SHA {
				sim = ph.Sim
			}
			fj := fileJSON{ID: ph.ID, Name: filepath.Base(ph.Path), Folder: rel, Size: ph.Size,
				W: ph.W, H: ph.H, Date: ph.ModTime.Format("Jan 2, 2006"), Sim: sim, Same: ph.SHA == s.Files[0].SHA,
				Blinks: ph.Blinks, Soft: ph.Soft, Sharpest: ph.Sharpest}
			for _, f := range ph.Faces {
				fj.Faces = append(fj.Faces, faceJSON{X: f.X, Y: f.Y, S: f.Size, Eyes: f.EyePos})
			}
			sj.Files = append(sj.Files, fj)
		}
		sets = append(sets, sj)
	}
	a.mu.Lock()
	gen := a.gen
	a.mu.Unlock()
	writeJSON(w, map[string]any{
		"gen": gen, "root": res.Root, "scanned": len(res.Photos), "unreadable": res.Unreadable, "remembered": res.Remembered, "faces": res.Faces,
		"level": res.Level.Name, "seconds": int(res.Took.Seconds()), "sets": sets,
	})
}

// moveSet turns the list of file IDs from the window into a checked set:
// only files that are in a duplicate set, and never every file of a set.
func (a *app) moveSet(res *scanResult, ids []int) (map[int]bool, string) {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	move := map[int]bool{}
	for _, s := range res.Sets {
		kept := 0
		for _, ph := range s.Files {
			if want[ph.ID] {
				move[ph.ID] = true
			} else {
				kept++
			}
		}
		if kept == 0 {
			return nil, fmt.Sprintf("Set %d has every photo marked to move. Keep at least one photo in each set.", s.ID)
		}
	}
	return move, ""
}

func (a *app) handleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	a.mu.Lock()
	res := a.res
	if res == nil || a.phase != "results" {
		a.mu.Unlock()
		writeJSON(w, map[string]string{"error": "There's nothing to move. Run a scan first."})
		return
	}
	move, problem := a.moveSet(res, req.IDs)
	if problem != "" || len(move) == 0 {
		a.mu.Unlock()
		if problem == "" {
			problem = "No photos are marked to move."
		}
		writeJSON(w, map[string]string{"error": problem})
		return
	}
	a.phase = "moving"
	a.prog = progress{Phase: "moving", Total: int64(len(move))}
	a.busyMove.Store(true)
	a.mu.Unlock()

	logf("Moving %d files into %s", len(move), filepath.Join(res.Root, "DUPES"))
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logf("Move crashed: %v", r)
				a.mu.Lock()
				a.outcome, a.phase = &moveOutcome{DupesDir: filepath.Join(res.Root, "DUPES"), Failed: []string{fmt.Sprint("Error: ", r)}}, "done"
				a.mu.Unlock()
				a.busyMove.Store(false)
			}
		}()
		out := moveFiles(res, move, func(d, t int) {
			a.mu.Lock()
			a.prog.Done, a.prog.Total = int64(d), int64(t)
			a.mu.Unlock()
		})
		logf("Moved %d files (%s), %d could not be moved", out.Moved, humanSize(out.Bytes), len(out.Failed))
		a.mu.Lock()
		a.prog.Phase = "sheet"
		a.mu.Unlock()
		if p, err := writeMovedSheet(res, &out); err == nil {
			out.Sheet = p
			logf("Spreadsheet saved: %s", p)
		} else {
			logf("Spreadsheet could not be saved: %v", err)
		}
		a.mu.Lock()
		a.outcome, a.phase = &out, "done"
		a.mu.Unlock()
		a.busyMove.Store(false)
	}()
	writeJSON(w, map[string]string{"ok": "1"})
}

func (a *app) handleReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int `json:"ids"`
	}
	readJSON(r, &req)
	a.mu.Lock()
	res := a.res
	a.mu.Unlock()
	if res == nil {
		writeJSON(w, map[string]string{"error": "Run a scan first."})
		return
	}
	move := map[int]bool{}
	for _, id := range req.IDs {
		move[id] = true
	}
	_, sheet, err := writeReports(res, move)
	if err != nil {
		writeJSON(w, map[string]string{"error": "The report couldn't be saved: " + err.Error()})
		return
	}
	openFile(sheet)
	writeJSON(w, map[string]string{"path": sheet})
}

func (a *app) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		What string `json:"what"` // dupes, root, reveal
		ID   int    `json:"id"`
	}
	readJSON(r, &req)
	a.mu.Lock()
	res, out := a.res, a.outcome
	a.mu.Unlock()
	switch req.What {
	case "dupes":
		if out != nil {
			openFile(out.DupesDir)
		}
	case "reports":
		if res != nil {
			openFile(reportsDir(res))
		} else {
			openFile(filepath.Join(documentsDir(), "DDupe Reports"))
		}
	case "sheet":
		if out != nil && out.Sheet != "" {
			openFile(out.Sheet)
		}
	case "root":
		if res != nil {
			openFile(res.Root)
		}
	case "reveal":
		if ph := a.photoByID(req.ID); ph != nil {
			revealFile(ph.Path)
		}
	}
	writeJSON(w, map[string]string{"ok": "1"})
}

// photoFor finds the photo a preview address asks for. The address carries
// the scan number (g), and a preview from an earlier scan is refused rather
// than showing the wrong photo.
func (a *app) photoFor(r *http.Request, prefix string) *photo {
	a.mu.Lock()
	gen := a.gen
	a.mu.Unlock()
	if r.URL.Query().Get("g") != strconv.Itoa(gen) {
		return nil
	}
	return a.photoByID(idFromPath(r.URL.Path, prefix))
}

func (a *app) photoByID(id int) *photo {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.res == nil || id < 0 || id >= len(a.res.Photos) {
		return nil
	}
	return a.res.Photos[id]
}

func idFromPath(p, prefix string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(p, prefix))
	if err != nil {
		return -1
	}
	return n
}

func (a *app) handleThumb(w http.ResponseWriter, r *http.Request) {
	ph := a.photoFor(r, "/thumb/")
	if ph == nil {
		http.NotFound(w, r)
		return
	}
	b, err := thumbnail(ph)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(b)
}

func (a *app) handleImage(w http.ResponseWriter, r *http.Request) {
	ph := a.photoFor(r, "/img/")
	if ph == nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(ph.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", ph.ModTime, f)
}
