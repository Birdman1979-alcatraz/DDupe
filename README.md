<p align="center"><img src="assets/icon-256.png" width="120" alt="DDupe icon"></p>

<h1 align="center">DDupe</h1>
<p align="center"><b>Free duplicate photo finder for Windows</b><br>
Finds exact copies <i>and</i> look-alikes in your JPEG archive. You choose what to keep. Nothing is ever deleted.</p>

<p align="center"><img src="assets/screen-results.png" width="820" alt="DDupe showing sets of duplicate photos, with the keeper circled in each set"></p>

---

## Download

Go to **[Releases](../../releases/latest)** and download **DDupe.exe**. That one file is the whole program: no installer, nothing else to download.

**Requirements:** Windows 10 or 11 (64-bit). DDupe opens in its own window using Microsoft Edge, which is built into Windows. You don't need Edge as your browser.

## Why DDupe exists

I worked for many years as a commercial and industrial photographer, and later as a portrait photographer. Digital photography creates mountains of clutter: backups, exports, re-saves and edits of the same picture. It has to be weeded out regularly, or you drown in it and keep buying more storage. I needed a duplicate finder for decades and never found one that worked well, so I built DDupe with Claude (an AI assistant). Claude wrote the code; I brought what photographers actually need. On its first day it freed almost a terabyte of storage for me.

## What it finds

- **Exact duplicates:** byte-for-byte identical files, even with different names.
- **Look-alikes:** the same picture saved again. That includes resized, re-compressed, brightened, colour-adjusted, black and white, rotated, date-stamped or slightly cropped copies.

## How to use it

1. **Open DDupe.exe** and click **Choose folder** (or paste a path, or pick a recent folder). Every subfolder is included.
2. **Choose how close a copy needs to be:**
   - **Strict:** only resized, re-saved or rotated copies
   - **Normal** (recommended): also edited, brightened, stamped or slightly cropped copies
   - **Loose:** also heavier crops and near-identical frames from the same moment (check these by eye)
3. **Scan.** A film-strip progress bar shows how far along it is. You can stop at any time.
4. **Review the contact sheet.** Each set of duplicates is a strip. The photo that stays is circled in grease pencil, and the others are marked to move.
   - **Click any photo** to switch it between keep and move. Every set always keeps at least one photo.
   - Filter by exact copies or look-alikes, or sort by most space saved.
5. **Look closer** with the magnifying-glass button on any photo (see below).
6. **Move.** Click **Move files** and confirm. The extras go into a **DUPES** folder inside your photo folder, keeping their subfolders.
7. **When you're happy,** delete the DUPES folder yourself to free the space. To clean another folder, click **New scan**.

## The magnifying glass

<p align="center"><img src="assets/screen-magnifier.png" width="720" alt="Full-size photo viewer with a magnifying glass showing detail at 100%"></p>

Open any photo full size, then **click on the photo** and a magnifying glass appears. It shows that spot at actual pixels (100%), so you can check focus and detail.

- Move the mouse to look around. Scroll to zoom to 200% or 300%. Click again to put it away.
- **The magnifier stays on the same spot when you switch photos** (arrow keys), so you can compare the same detail across copies and keep the sharpest one.
- Keyboard: **←/→** photos in the set, **Page Up/Down** other sets, **K** keep, **M** move, **Z** magnifier, **Esc** close.
- **Show in folder** opens the photo in File Explorer.

## Spreadsheets and history

- After every move, DDupe quietly saves an **Excel spreadsheet** to **Documents\DDupe Reports**. It never opens by itself.
  - **Summary:** folder, date, matching level, photos checked, files moved, space used
  - **Moved files:** where each file came from, where it is now, the copy that was kept, size, dimensions, date and match %
  - **All duplicates:** every photo in every set, marked kept or moved
  - File paths are clickable, so you can open any photo straight from Excel. Headers stay in place, and every column can be filtered.
- The reports folder is separate from your photos on purpose: **the spreadsheets stay after you delete DUPES**, so you keep a lasting log book of every cleanup.
- Click **Past reports** in the top bar to open that folder. **Save report** creates a spreadsheet and a printable contact sheet *before* you move anything.
- A plain-text list of moves is also kept in `DUPES\moved_files_log.txt`.

## Your photos are safe

- DDupe **never deletes or changes your photos.** It only moves duplicates into DUPES, after you confirm.
- It never connects to the internet. Its window talks only to DDupe itself on your own computer, protected by a secret key, so other programs and websites can't use it.
- If anything interrupts a move, nothing is lost: files already moved are in DUPES, and the rest are where they were. Scan the same folder again and DDupe picks up where it left off, because DUPES is always skipped.

## Windows and antivirus warnings

DDupe is free and isn't signed with a paid certificate, so security software doesn't know it yet.

**"Windows protected your PC":** click **More info → Run anyway** the first time.

**Trend Micro and other antivirus.** A new program that reads and moves many photos looks like ransomware to security software. Trend Micro's **Folder Shield** may pause or close DDupe and ask whether to trust it.

- If that happens during a move, DDupe shows a **"Waiting for DDupe"** screen. Trust DDupe, and the window carries on by itself. If DDupe was closed, open it again and scan the same folder.
- **To stop this permanently, add DDupe to Folder Shield's trusted list** ([Trend Micro's instructions](https://helpcenter.trendlife.com/en-us/article/TMKA-19477)):
  1. Open Trend Micro and go to the **Data** tab.
  2. Click **Configure** next to **Folder Shield**.
  3. Click **Trusted Program List → Add → Browse**, and choose DDupe.exe.
  4. Click **Done**.
- **Keep DDupe.exe in one permanent folder** (for example `Documents\DDupe`) and replace it there when you update. Each new version is a new file, so your antivirus may ask once more after an update.
- **Don't turn your antivirus off.** Trusting DDupe is enough.

## If something goes wrong

DDupe keeps an activity log at:

```
%LOCALAPPDATA%\DDupe\DDupe-log.txt
```

Paste that address into File Explorer's address bar to find it. It records each scan, each move, any file that couldn't be moved and why, and any crash. Please attach it when you report a problem.

## Known limitations

- JPEG only (.jpg, .jpeg, .jpe, .jfif). RAW, PNG, TIFF and HEIC are not checked yet.
- Heavily cropped images, and burst frames where the camera moved noticeably, may not be matched.
- Some unusual JPEGs (from old cameras or scanners) can't be read. They're skipped and counted in the results.

## Comments and suggestions

Welcome! Please open an **[Issue](../../issues)**.

---

## For developers

### Building

Requires [Go](https://go.dev/dl/) 1.23 or newer. No third-party libraries are used.

```
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-H windowsgui" -o DDupe.exe .
```

`-H windowsgui` makes it a window app (no console). The icon, version details and Windows manifest are pre-compiled in `rsrc_windows_amd64.syso`. To change them, edit `app.rc`, `app.manifest` or `DDupe.ico` and regenerate it, for example with LLVM:

```
llvm-rc -no-preprocess -fo app.res app.rc
llvm-cvtres /machine:x64 /out:rsrc_windows_amd64.syso app.res
```

It also builds and runs on Linux and macOS for development. Set `DDUPE_NO_WINDOW=1` to print the address instead of opening a window.

### How it's put together

| File | What it does |
|---|---|
| `main.go` | Starts a local web server on `127.0.0.1` (random port, secret token) and opens it in an Edge app window. Closes itself a few seconds after the window closes. |
| `engine.go` | Finds photos, fingerprints them in parallel, groups duplicates, moves files. |
| `hash.go` | Look-alike fingerprints (see below) and EXIF rotation. |
| `thumbs.go` | Makes preview thumbnails on demand, with a memory cache. |
| `xlsx.go` | Writes Excel files directly (formatted headers, filters, frozen rows, clickable links). |
| `report.go` | Printable HTML contact sheet; report file names and the reports folder. |
| `logging.go` | Activity and crash log. |
| `native_windows.go` | Windows folder picker, opening files and folders, finding Documents, launching the app window. |
| `ui/index.html` | The whole interface (HTML, CSS and JavaScript, embedded in the .exe). |

### How look-alike matching works

Each photo is shrunk to a 32×32 grayscale thumbnail by averaging every pixel, after applying the camera's EXIF rotation. Two fingerprints are computed from it: a DCT-based perceptual hash and a difference hash. Two photos are look-alikes only when **both** are close enough for the chosen level. Sets are built around the largest photo, and every member must closely match it, so unrelated photos can't be chained together.

### Code signing (future)

The lasting fix for SmartScreen and antivirus warnings is signing each release. [Microsoft's Artifact Signing](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options) (formerly Trusted Signing) costs about US$10 a month and is open to individual developers in the USA and Canada. It doesn't give instant trust, but signing every release with the same identity builds a reputation over time.

## License

Free to use, share and modify under the [MIT License](LICENSE).
