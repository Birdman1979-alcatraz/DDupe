# DDupe version history

## 2.4
- **Face checks in look-alike sets:** flags **eyes closed** and **soft focus**, marks the frame with the **sharpest faces**, and keeps the eyes-open, sharpest frame when the usual keeper has a blink or soft faces. Runs entirely on your computer using the open-source pigo face finder.
- **Show faces found** in the full-size viewer (or press **F**) draws each face and eye DDupe found.
- **Whole-drive scanning:** Windows' system folders, app data, the Recycle Bin and Lightroom preview caches are skipped automatically.
- **Fingerprint memory:** unchanged photos aren't read again, so rescans are dramatically faster and a stopped scan picks up where it left off.
- The spreadsheets and printable report gain a **Face check** column.

## 2.3
- Spreadsheets are now saved to **Documents\DDupe Reports** instead of inside DUPES, so they survive when you delete the DUPES folder and build up a lasting history of every cleanup.
- The finish screen mentions the spreadsheet with a small **View it** link instead of a button. Spreadsheets never open by themselves.
- New **Past reports** link in the top bar opens the reports folder.
- Report files are named by date and folder, for example `2026-10-17 0915 Smith wedding - Moved.xlsx`.

## 2.2
- Fixed: the window could freeze after clicking **Move** when Trend Micro (or other security software) paused DDupe to ask whether to trust it. The window now shows **"Waiting for DDupe"** and carries on by itself once DDupe is trusted. If DDupe was closed, rescanning the same folder picks up where it left off.
- New activity log at `%LOCALAPPDATA%\DDupe\DDupe-log.txt`, which records scans, moves, files that couldn't be moved, and crashes.
- Faster spreadsheet writing for very large libraries.

## 2.1
- **Magnifying glass:** click a photo in the full-size viewer to see that spot at actual pixels. Scroll to zoom to 200% or 300%. It stays on the same spot as you flip between copies, so you can compare sharpness.
- The magnifying-glass button is always visible on every photo in the contact sheet.
- **Excel spreadsheet** (.xlsx) saved after every move, with Summary, Moved files and All duplicates tabs and clickable links to every photo.
- **Save report** creates an Excel spreadsheet and a printable contact sheet before anything moves.

## 2.0
- A full window interface replaces the black command window. It uses Microsoft Edge's app mode, so there's nothing to install.
- Modern Windows folder picker, a recent-folders list, and a choice of matching level on the start screen.
- Film-strip progress bar with time remaining, and a **Stop scan** button.
- Contact-sheet results: the keeper is circled in grease pencil, and you click any photo to switch it between keep and move. Each set always keeps at least one photo.
- Full-size viewer with filmstrip, details and **Show in folder**.
- Filter by exact copies or look-alikes, and sort by folder, space saved or number of copies.
- Confirmation before moving, and a finish screen with **Open DUPES folder**.

## 1.4: first public release
- Console program: finds exact duplicates and look-alikes, opens an HTML contact sheet in your browser, and optionally moves extras to DUPES.
- Strict, Normal and Loose matching. EXIF rotation handled.
