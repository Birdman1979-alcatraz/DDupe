package main

import (
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// reportsDir is where DDupe keeps its spreadsheets: Documents\DDupe Reports.
// Keeping them outside the photo folder means they survive when the user
// deletes the DUPES folder, giving a lasting history of every clean-up.
func reportsDir(res *scanResult) string {
	dir := filepath.Join(documentsDir(), "DDupe Reports")
	if os.MkdirAll(dir, 0755) == nil {
		return dir
	}
	return res.Root // fall back to the photo folder
}

// reportName builds a file name like "2026-10-17 0915 Smith wedding - Moved".
func reportName(res *scanResult, kind string) string {
	name := filepath.Base(res.Root)
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == "_" {
		name = "Photos"
	}
	return time.Now().Format("2006-01-02 1504") + " " + name + " - " + kind
}

// writeReports saves the "Save report" spreadsheet and a printable HTML
// contact sheet. move holds the files currently marked to move.
func writeReports(res *scanResult, move map[int]bool) (htmlPath, sheetPath string, err error) {
	base := filepath.Join(reportsDir(res), reportName(res, "Plan"))
	htmlPath, sheetPath = base+".html", base+".xlsx"
	if err = writeHTML(htmlPath, res, move); err != nil {
		return "", "", err
	}
	err = writePlanSheet(sheetPath, res, move)
	return htmlPath, sheetPath, err
}

func kindLabel(s *dupSet) string {
	if s.Kind == "exact" {
		return "Exact duplicate"
	}
	return "Look-alike"
}

func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

func writeHTML(path string, res *scanResult, move map[int]bool) error {
	var sb strings.Builder
	var nMove int
	var bytesMove int64
	for _, s := range res.Sets {
		for _, ph := range s.Files {
			if move[ph.ID] {
				nMove++
				bytesMove += ph.Size
			}
		}
	}
	sb.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>DDupe report</title><style>
:root{--bg:#f4f4f4;--card:#fff;--ink:#1e1e1e;--muted:#666;--line:#ddd;--mark:#d9472b}
@media (prefers-color-scheme:dark){:root{--bg:#2e2e2e;--card:#383838;--ink:#ececec;--muted:#a6a6a6;--line:#4a4a4a;--mark:#f0663f}}
body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.5 "Segoe UI",system-ui,sans-serif}
main{max-width:1200px;margin:0 auto;padding:24px 16px 64px}
h1{font:600 30px/1.2 Bahnschrift,"Segoe UI",sans-serif;margin:0 0 6px}
.sub{color:var(--muted);margin:0 0 24px;word-break:break-all}
.set{background:var(--card);border:1px solid var(--line);border-radius:6px;padding:14px;margin:14px 0;break-inside:avoid}
.set h2{margin:0 0 10px;font:600 15px Bahnschrift,"Segoe UI",sans-serif;color:var(--muted)}
.grid{display:flex;flex-wrap:wrap;gap:14px}
.item{width:200px}
.item img{width:200px;height:150px;object-fit:contain;background:var(--bg);border-radius:3px}
.move img{opacity:.5}
.tag{font-size:12px;font-weight:700;color:var(--muted)}.keep .tag{color:var(--ink)}.move .tag{color:var(--mark)}
.path{font-size:12px;word-break:break-all}.path a{color:inherit}
.meta{font-size:12px;color:var(--muted)}
</style></head><body><main>`)
	fmt.Fprintf(&sb, `<h1>DDupe report</h1><p class="sub">%s<br>%s. %d photos scanned, %d duplicate sets, %d files marked to move (%s). Matching level: %s.</p>`,
		html.EscapeString(res.Root), time.Now().Format("January 2, 2006 3:04 PM"), len(res.Photos), len(res.Sets), nMove, humanSize(bytesMove), res.Level.Name)
	for _, s := range res.Sets {
		fmt.Fprintf(&sb, `<section class="set"><h2>Set %d, %s, %d files</h2><div class="grid">`, s.ID, strings.ToLower(kindLabel(s)), len(s.Files))
		for i, ph := range s.Files {
			cls, label := "keep", "Keep"
			if move[ph.ID] {
				cls, label = "move", "Move to DUPES"
			}
			if s.Kind == "similar" && i > 0 && ph.SHA != s.Files[0].SHA {
				label += fmt.Sprintf(", %d%% match", ph.Sim)
			}
			u := html.EscapeString(fileURL(ph.Path))
			fmt.Fprintf(&sb, `<div class="item %s"><a href="%s" target="_blank"><img loading="lazy" src="%s" alt=""></a><div class="tag">%s</div><div class="path"><a href="%s" target="_blank">%s</a></div><div class="meta">%d×%d, %s, %s</div></div>`,
				cls, u, u, label, u, html.EscapeString(ph.Path), ph.W, ph.H, humanSize(ph.Size), ph.ModTime.Format("Jan 2, 2006"))
		}
		sb.WriteString(`</div></section>`)
	}
	if len(res.Sets) == 0 {
		sb.WriteString(`<p>No duplicates found.</p>`)
	}
	sb.WriteString(`</main></body></html>`)
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<40:
		return fmt.Sprintf("%.2f TB", float64(b)/(1<<40))
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", b)
}
