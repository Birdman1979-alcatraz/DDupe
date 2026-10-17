## DDupe 2.4: keeps your best shot 📸

DDupe is a free, no-install duplicate photo finder for Windows 10 and 11. It finds exact copies and look-alikes in your JPEG archive, lets you choose what to keep, and never deletes anything.

### What's new
- **Blink and focus checks.** When DDupe finds several frames of the same moment, such as a burst or near-identical portraits, it looks at the faces. Frames where someone's **eyes are closed** or the faces are **soft** are flagged, and DDupe keeps the frame with **open eyes and the sharpest faces**. You can still click to change it.
- **Never fooled by edits.** A brightened, black and white or recompressed copy has the same eyes, so it is never called a blink. Faces are only compared within a set of the same moment.
- **Show faces found.** In the full-size viewer, tick it (or press **F**) to see each face and eye DDupe found.
- **Scan a whole drive.** Pick `D:\` and DDupe scans everything, automatically skipping Windows' system folders, app data, the Recycle Bin and Lightroom preview caches.
- **Much faster rescans.** DDupe remembers the photos it has already read, so a second scan of the same drive takes seconds or minutes instead of an hour. A stopped scan picks up where it left off.
- **Face check column** in the Excel spreadsheet and printable report.

### Private by design
Everything happens on your computer, and nothing is uploaded. Face checks don't identify or name anyone; they only compare frames of the same moment. The face finder is the open-source [pigo](https://github.com/esimov/pigo) library (MIT licence).

### First run
- **"Windows protected your PC":** click **More info → Run anyway**.
- **Trend Micro:** this is a new file, so add it to Folder Shield's **Trusted Program List** again (Data tab → Folder Shield → Configure → Trusted Program List → Add).

**Requirements:** Windows 10 or 11, 64-bit. Nothing to install.

Comments and suggestions are welcome. Please open an Issue.
