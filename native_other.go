//go:build !windows

package main

import (
	"fmt"
	"os"
)

// Stand-ins so DDupe can be developed and tested on other systems.
func pickFolder() string            { return os.Getenv("DDUPE_PICK") }
func openFile(p string)             { fmt.Println("open:", p) }
func revealFile(p string)           { fmt.Println("reveal:", p) }
func openAppWindow(url string) bool { fmt.Println(url); return true }
func showError(msg string)          { fmt.Println(msg) }

func documentsDir() string {
	home, _ := os.UserHomeDir()
	return home + "/Documents"
}
