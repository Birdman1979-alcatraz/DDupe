package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// DDupe keeps a short activity log so problems (for example security
// software stopping the program) can be traced afterwards:
//   %LOCALAPPDATA%\DDupe\DDupe-log.txt

var (
	logMu   sync.Mutex
	logFile *os.File
)

func logPath() string {
	dir, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "DDupe", "DDupe-log.txt")
}

func startLog() {
	p := logPath()
	os.MkdirAll(filepath.Dir(p), 0755)
	if st, err := os.Stat(p); err == nil && st.Size() > 2<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	logFile = f
	debug.SetCrashOutput(f, debug.CrashOptions{}) // record crashes too
	logf("DDupe %s started", version)
}

func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, "%s  %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
	logFile.Sync()
}
