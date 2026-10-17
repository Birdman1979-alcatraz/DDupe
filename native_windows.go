//go:build windows

package main

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	ole32             = syscall.NewLazyDLL("ole32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	procBrowse        = shell32.NewProc("SHBrowseForFolderW")
	procPathFromID    = shell32.NewProc("SHGetPathFromIDListW")
	procShellExecute  = shell32.NewProc("ShellExecuteW")
	procCoInitialize  = ole32.NewProc("CoInitializeEx")
	procCoUninit      = ole32.NewProc("CoUninitialize")
	procCoCreate      = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree = ole32.NewProc("CoTaskMemFree")
	procForeground    = user32.NewProc("GetForegroundWindow")
	procMessageBox    = user32.NewProc("MessageBoxW")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidFileOpenDialog = guid{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = guid{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// comCall calls method number idx of a COM object.
func comCall(obj uintptr, idx int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

// pickFolder shows the modern Windows folder picker (falls back to the
// classic one on very old systems). Returns "" if the user cancels.
func pickFolder() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	procCoInitialize.Call(0, 0x2|0x4) // apartment-threaded, no OLE1
	defer procCoUninit.Call()
	owner, _, _ := procForeground.Call()

	var dlg uintptr
	hr, _, _ := procCoCreate.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1, /*CLSCTX_INPROC_SERVER*/
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if hr != 0 || dlg == 0 {
		return pickFolderClassic(owner)
	}
	defer comCall(dlg, 2) // Release

	var opts uint32
	comCall(dlg, 10, uintptr(unsafe.Pointer(&opts)))                          // GetOptions
	comCall(dlg, 9, uintptr(opts|0x20|0x40|0x800))                            // SetOptions: pick folders, file system only, path must exist
	comCall(dlg, 17, uintptr(unsafe.Pointer(utf16("Choose a photo folder")))) // SetTitle
	comCall(dlg, 18, uintptr(unsafe.Pointer(utf16("Use this folder"))))       // SetOkButtonLabel
	if comCall(dlg, 3, owner) != 0 {                                          // Show (non-zero = cancelled or failed)
		return ""
	}
	var item uintptr
	if comCall(dlg, 20, uintptr(unsafe.Pointer(&item))) != 0 || item == 0 { // GetResult
		return ""
	}
	defer comCall(item, 2)
	var name *uint16
	if comCall(item, 5, 0x80058000 /*SIGDN_FILESYSPATH*/, uintptr(unsafe.Pointer(&name))) != 0 || name == nil {
		return ""
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return utf16PtrToString(name)
}

func utf16PtrToString(p *uint16) string {
	var s []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		s = append(s, c)
	}
	return syscall.UTF16ToString(s)
}

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

func pickFolderClassic(owner uintptr) string {
	display := make([]uint16, 260)
	bi := browseInfo{Owner: owner, DisplayName: &display[0], Title: utf16("Choose a photo folder"), Flags: 0x0001 | 0x0040 | 0x0200}
	pidl, _, _ := procBrowse.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 32768)
	if ok, _, _ := procPathFromID.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); ok == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func shellExecute(file, params string) bool {
	var p uintptr
	if params != "" {
		p = uintptr(unsafe.Pointer(utf16(params)))
	}
	r, _, _ := procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(file))), p, 0, 1)
	return r > 32
}

// openFile opens a file or folder the same way double-clicking it would.
func openFile(p string) { shellExecute(p, "") }

// revealFile opens File Explorer with the file selected.
func revealFile(p string) { shellExecute("explorer.exe", `/select,"`+p+`"`) }

// openAppWindow shows DDupe in an Edge (or Chrome) app window, which has no
// address bar or tabs. Falls back to the default browser.
func openAppWindow(url string) bool {
	args := "--app=" + url + " --window-size=1360,900"
	return shellExecute("msedge.exe", args) || shellExecute("chrome.exe", args) || shellExecute(url, "")
}

var folderIDDocuments = guid{0xFDD39AD0, 0x238F, 0x46AF, [8]byte{0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7}}

// documentsDir finds the real Documents folder, even when OneDrive or the
// user has moved it somewhere else.
func documentsDir() string {
	var p *uint16
	r, _, _ := shell32.NewProc("SHGetKnownFolderPath").Call(uintptr(unsafe.Pointer(&folderIDDocuments)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 && p != nil {
		defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
		return utf16PtrToString(p)
	}
	home, _ := os.UserHomeDir()
	return home + `\Documents`
}

func showError(msg string) {
	procMessageBox.Call(0, uintptr(unsafe.Pointer(utf16(msg))), uintptr(unsafe.Pointer(utf16("DDupe"))), 0x10)
}
