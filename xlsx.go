package main

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A small Excel (.xlsx) writer: enough for formatted tables with a bold
// header row, frozen headers, filters, column widths, numbers, dates and
// clickable links to files.

type cell struct {
	text  string
	num   float64
	isNum bool
	style int    // index into cellXfs in styles.xml
	link  string // file path or URL to open when clicked
}

const (
	stText = iota
	stHeader
	stLink
	stMB
	stInt
	stDate
	stLabel
	stTitle
	stPct
)

func txt(s string) cell              { return cell{text: s} }
func lbl(s string) cell              { return cell{text: s, style: stLabel} }
func link(s, target string) cell     { return cell{text: s, style: stLink, link: target} }
func numCell(v float64, st int) cell { return cell{num: v, isNum: true, style: st} }
func intCell(v int) cell             { return numCell(float64(v), stInt) }
func mbCell(b int64) cell            { return numCell(float64(b)/(1<<20), stMB) }
func dateCell(t time.Time) cell      { return numCell(excelDate(t), stDate) }

type sheet struct {
	name   string
	widths []float64
	rows   [][]cell
	filter bool // header row + autofilter + frozen header
}

func excelDate(t time.Time) float64 {
	t = t.Local()
	_, off := t.Zone()
	return float64(t.Unix()+int64(off))/86400 + 25569
}

func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

func xmlEsc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func writeXLSX(path string, sheets []sheet) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	put := func(name, body string) {
		w, _ := z.Create(name)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + body))
	}

	var ct, wbSheets, wbRels, defNames strings.Builder
	for i, sh := range sheets {
		n := i + 1
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&wbSheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEsc(sh.name), n, n)
		fmt.Fprintf(&wbRels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, n, n)
		if sh.filter && len(sh.rows) > 0 {
			fmt.Fprintf(&defNames, `<definedName name="_xlnm._FilterDatabase" localSheetId="%d" hidden="1">'%s'!$A$1:$%s$%d</definedName>`,
				i, xmlEsc(sh.name), colName(len(sh.rows[0])-1), len(sh.rows))
		}
		writeSheet(z, n, sh)
	}
	stylesID := len(sheets) + 1

	put("[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
		`<Default Extension="xml" ContentType="application/xml"/>`+
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`+
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`+
		`<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>`+
		ct.String()+`</Types>`)
	put("_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>`+
		`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>`+
		`</Relationships>`)
	put("docProps/core.xml", `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">`+
		`<dc:title>DDupe report</dc:title><dc:creator>DDupe</dc:creator>`+
		`<dcterms:created xsi:type="dcterms:W3CDTF">`+time.Now().UTC().Format("2006-01-02T15:04:05Z")+`</dcterms:created></cp:coreProperties>`)
	dn := ""
	if defNames.Len() > 0 {
		dn = "<definedNames>" + defNames.String() + "</definedNames>"
	}
	put("xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<bookViews><workbookView/></bookViews><sheets>`+wbSheets.String()+`</sheets>`+dn+`</workbook>`)
	put("xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+wbRels.String()+
		fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, stylesID)+
		`</Relationships>`)
	put("xl/styles.xml", `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`+
		`<numFmts count="3"><numFmt numFmtId="164" formatCode="#,##0.0"/><numFmt numFmtId="165" formatCode="yyyy-mm-dd hh:mm"/><numFmt numFmtId="166" formatCode="0&quot;%&quot;"/></numFmts>`+
		`<fonts count="5">`+
		`<font><sz val="11"/><name val="Calibri"/><family val="2"/></font>`+
		`<font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/><family val="2"/></font>`+
		`<font><u/><sz val="11"/><color rgb="FF0563C1"/><name val="Calibri"/><family val="2"/></font>`+
		`<font><b/><sz val="11"/><name val="Calibri"/><family val="2"/></font>`+
		`<font><b/><sz val="16"/><name val="Calibri"/><family val="2"/></font>`+
		`</fonts>`+
		`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>`+
		`<fill><patternFill patternType="solid"><fgColor rgb="FF3A3A3A"/><bgColor indexed="64"/></patternFill></fill></fills>`+
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>`+
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`+
		`<cellXfs count="9">`+
		`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`+ // text
		`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>`+ // header
		`<xf numFmtId="0" fontId="2" fillId="0" borderId="0" xfId="0" applyFont="1"/>`+ // link
		`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`+ // MB
		`<xf numFmtId="3" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`+ // integer
		`<xf numFmtId="165" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`+ // date
		`<xf numFmtId="0" fontId="3" fillId="0" borderId="0" xfId="0" applyFont="1"/>`+ // bold label
		`<xf numFmtId="0" fontId="4" fillId="0" borderId="0" xfId="0" applyFont="1"/>`+ // title
		`<xf numFmtId="166" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`+ // percent
		`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	if err := z.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func writeSheet(z *zip.Writer, n int, sh sheet) {
	var b, rels, links strings.Builder
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	b.WriteString(`<sheetPr><pageSetUpPr fitToPage="1"/></sheetPr>`)
	if sh.filter {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	} else {
		b.WriteString(`<sheetViews><sheetView workbookViewId="0"/></sheetViews>`)
	}
	if len(sh.widths) > 0 {
		b.WriteString("<cols>")
		for i, w := range sh.widths {
			fmt.Fprintf(&b, `<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, w)
		}
		b.WriteString("</cols>")
	}
	b.WriteString("<sheetData>")
	nLink := 0
	for r, row := range sh.rows {
		fmt.Fprintf(&b, `<row r="%d">`, r+1)
		for c, cl := range row {
			ref := fmt.Sprintf("%s%d", colName(c), r+1)
			st := cl.style
			if sh.filter && r == 0 {
				st = stHeader
			}
			if cl.isNum {
				fmt.Fprintf(&b, `<c r="%s" s="%d"><v>%g</v></c>`, ref, st, cl.num)
			} else if cl.text != "" {
				fmt.Fprintf(&b, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, st, xmlEsc(cl.text))
			}
			if cl.link != "" {
				nLink++
				fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, nLink, xmlEsc(cl.link))
				fmt.Fprintf(&links, `<hyperlink ref="%s" r:id="rId%d"/>`, ref, nLink)
			}
		}
		b.WriteString("</row>")
	}
	b.WriteString("</sheetData>")
	if sh.filter && len(sh.rows) > 0 {
		fmt.Fprintf(&b, `<autoFilter ref="A1:%s%d"/>`, colName(len(sh.rows[0])-1), len(sh.rows))
	}
	if nLink > 0 {
		b.WriteString("<hyperlinks>" + links.String() + "</hyperlinks>")
	}
	b.WriteString(`<pageMargins left="0.5" right="0.5" top="0.75" bottom="0.75" header="0.3" footer="0.3"/><pageSetup orientation="landscape" fitToWidth="1" fitToHeight="0"/></worksheet>`)

	w, _ := z.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", n))
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + b.String()))
	if nLink > 0 {
		w, _ := z.Create(fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", n))
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`))
	}
}

// fileLink makes a link Excel can open, for a file or folder on this computer.
func fileLink(p string) string {
	if filepath.Separator == '\\' {
		return p
	}
	return "file://" + p
}

// ----- DDupe's spreadsheets -----

func summarySheet(res *scanResult, rows [][]cell) sheet {
	sum := [][]cell{
		{cell{text: "DDupe report", style: stTitle}},
		{},
		{lbl("Photo folder"), link(res.Root, fileLink(res.Root))},
		{lbl("Report created"), txt(time.Now().Format("January 2, 2006 3:04 PM"))},
		{lbl("Matching level"), txt(res.Level.Name)},
		{lbl("Photos checked"), txt(commas(len(res.Photos)))},
		{lbl("Duplicate sets found"), txt(commas(len(res.Sets)))},
	}
	if res.Unreadable > 0 {
		sum = append(sum, []cell{lbl("Files that couldn't be read"), txt(commas(res.Unreadable))})
	}
	sum = append(sum, rows...)
	return sheet{name: "Summary", widths: []float64{30, 90}, rows: sum}
}

var allHeader = []cell{txt("Set"), txt("Type"), txt("Action"), txt("File name"), txt("Folder it is in now"), txt("Open photo"),
	txt("Size (MB)"), txt("Width"), txt("Height"), txt("Modified"), txt("Match %"), txt("Face check")}
var allWidths = []float64{6, 16, 16, 32, 50, 12, 10, 8, 8, 17, 9, 28}

func photoRow(s *dupSet, i int, ph *photo, action, where string) []cell {
	match := cell{}
	if s.Kind == "similar" && i > 0 && ph.SHA != s.Files[0].SHA {
		match = numCell(float64(ph.Sim), stPct)
	} else if i > 0 {
		match = numCell(100, stPct)
	}
	return []cell{intCell(s.ID), txt(kindLabel(s)), txt(action), txt(filepath.Base(ph.Path)), txt(filepath.Dir(where)),
		link("Open", fileLink(where)), mbCell(ph.Size), intCell(ph.W), intCell(ph.H), dateCell(ph.ModTime), match, txt(faceNote(ph))}
}

// faceNote describes the face check result for a photo, for the reports.
func faceNote(ph *photo) string {
	var n []string
	switch {
	case ph.Blinks == 1:
		n = append(n, "Eyes closed")
	case ph.Blinks > 1:
		n = append(n, fmt.Sprintf("%d people blinking", ph.Blinks))
	}
	if ph.Soft {
		n = append(n, "Soft focus")
	}
	if ph.Sharpest {
		n = append(n, "Sharpest faces")
	}
	return strings.Join(n, ", ")
}

// writeMovedSheet saves DUPES\DDupe_Moved_<date>.xlsx after a move.
func writeMovedSheet(res *scanResult, out *moveOutcome) (string, error) {
	path := filepath.Join(reportsDir(res), reportName(res, "Moved")+".xlsx")

	moved := [][]cell{{txt("Set"), txt("Type"), txt("File name"), txt("Moved from"), txt("Now in DUPES"), txt("Copy that was kept"),
		txt("Size (MB)"), txt("Width"), txt("Height"), txt("Modified"), txt("Match %"), txt("Face check")}}
	var failed [][]cell
	for _, r := range out.Records {
		i := 0
		for j, f := range r.Set.Files {
			if f == r.Photo {
				i = j
			}
		}
		match := cell{}
		if r.Set.Kind == "similar" && r.Photo.SHA != r.Set.Files[0].SHA {
			match = numCell(float64(r.Photo.Sim), stPct)
		} else if i > 0 {
			match = numCell(100, stPct)
		}
		kept := cell{}
		if len(r.Kept) > 0 {
			kept = link(r.Kept[0], fileLink(r.Kept[0]))
		}
		if r.To == "" {
			failed = append(failed, []cell{intCell(r.Set.ID), txt(r.Reason), txt(filepath.Base(r.From)), link(r.From, fileLink(r.From)),
				txt("Not moved: the file may be open in another program or read-only")})
			continue
		}
		moved = append(moved, []cell{intCell(r.Set.ID), txt(r.Reason), txt(filepath.Base(r.From)), txt(filepath.Dir(r.From)),
			link(r.To, fileLink(r.To)), kept, mbCell(r.Photo.Size), intCell(r.Photo.W), intCell(r.Photo.H), dateCell(r.Photo.ModTime), match, txt(faceNote(r.Photo))})
	}

	newPlace := map[*photo]string{}
	for _, r := range out.Records {
		if r.To != "" {
			newPlace[r.Photo] = r.To
		}
	}
	all := [][]cell{allHeader}
	for _, s := range res.Sets {
		for i, ph := range s.Files {
			action, where := "Kept", ph.Path
			if to, ok := newPlace[ph]; ok {
				action, where = "Moved to DUPES", to
			}
			all = append(all, photoRow(s, i, ph, action, where))
		}
	}

	sumRows := [][]cell{
		{lbl("Files moved to DUPES"), txt(commas(out.Moved))},
		{lbl("Space they use"), txt(humanSize(out.Bytes))},
		{lbl("DUPES folder"), link(out.DupesDir, fileLink(out.DupesDir))},
	}
	if len(out.Failed) > 0 {
		sumRows = append(sumRows, []cell{lbl("Files that couldn't be moved"), txt(commas(len(out.Failed)))})
	}
	sumRows = append(sumRows, []cell{}, []cell{txt("Nothing was deleted. Delete the DUPES folder yourself once you've checked it.")})

	sheets := []sheet{
		summarySheet(res, sumRows),
		{name: "Moved files", filter: true, rows: moved, widths: []float64{6, 16, 32, 50, 70, 70, 10, 8, 8, 17, 9, 28}},
		{name: "All duplicates", filter: true, rows: all, widths: allWidths},
	}
	if len(failed) > 0 {
		failed = append([][]cell{{txt("Set"), txt("Type"), txt("File name"), txt("Location"), txt("Problem")}}, failed...)
		sheets = append(sheets, sheet{name: "Not moved", filter: true, rows: failed, widths: []float64{6, 16, 32, 70, 60}})
	}
	return path, writeXLSX(path, sheets)
}

// writePlanSheet is the "Save report" spreadsheet, made before anything moves.
func writePlanSheet(path string, res *scanResult, move map[int]bool) error {
	all := [][]cell{allHeader}
	var n int
	var bytes int64
	for _, s := range res.Sets {
		for i, ph := range s.Files {
			action := "Keep"
			if move[ph.ID] {
				action = "Move to DUPES"
				n++
				bytes += ph.Size
			}
			all = append(all, photoRow(s, i, ph, action, ph.Path))
		}
	}
	sumRows := [][]cell{
		{lbl("Files marked to move"), txt(commas(n))},
		{lbl("Space they use"), txt(humanSize(bytes))},
		{},
		{txt("Nothing has been moved yet. This is the plan as marked in DDupe.")},
	}
	return writeXLSX(path, []sheet{summarySheet(res, sumRows), {name: "All duplicates", filter: true, rows: all, widths: allWidths}})
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
