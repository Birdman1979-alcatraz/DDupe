## DDupe 2.3: now with a full window interface 🎉

DDupe is a free, no-install duplicate photo finder for Windows 10 and 11. It's ideal for large JPEG archives, from family photos to commercial disk farms, to find duplicates and free up disk space. I worked as a commercial photographer for many years and needed a tool like this for a long time, but could never find a reliable one. I built DDupe with Claude: Claude wrote all the code, and I brought what a photographer, photojournalist, portrait photographer or amateur actually needs.

### What's new since 1.4
- **A real window instead of the black command window.** Choose a folder, pick a matching level, and watch a film-strip progress bar.
- **Contact-sheet results.** The keeper in each set is circled in grease pencil. Click any photo to switch it between keep and move, so you decide which copy stays.
- **Magnifying glass.** Open any photo full size and click to see it at actual pixels. It stays on the same spot as you flip between copies, so you can compare sharpness.
- **Excel spreadsheet after every move,** saved quietly to `Documents\DDupe Reports`. It lists where each file came from, where it went and which copy was kept, with clickable links. It stays after you delete DUPES, so you keep a history of every cleanup.
- **Handles antivirus interruptions.** If Trend Micro or similar pauses DDupe mid-move, the window waits and carries on once you trust DDupe.
- **Activity log** at `%LOCALAPPDATA%\DDupe\DDupe-log.txt` for troubleshooting.

### Non-destructive by design
DDupe never deletes or changes your photos. It only moves duplicates into a **DUPES** folder inside your photo folder, after you confirm. Deleting DUPES is always up to you.

### First run
- **"Windows protected your PC":** click **More info → Run anyway**.
- **Trend Micro:** add DDupe.exe to Folder Shield's **Trusted Program List** (Data tab → Folder Shield → Configure → Trusted Program List → Add). Keep DDupe.exe in one permanent folder. Full details are in the README.

### Known limitations
JPEG only for now (no RAW, PNG, TIFF or HEIC). Heavily cropped images and burst frames where the camera moved may not be matched.

**Requirements:** Windows 10 or 11, 64-bit. Nothing to install.

Have fun! Comments and suggestions are welcome. Please open an Issue.
