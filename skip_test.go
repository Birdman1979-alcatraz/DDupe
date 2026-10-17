package main

import "testing"

func TestSkip(t *testing.T) {
	cases := map[string]bool{
		`/Windows`: true, `/Program Files`: true, `/Photos/Windows`: false, `/Users/x/AppData`: true,
		`/Photos/DUPES`: true, `/Photos/Catalog Previews.lrdata`: true, `/Photos/2019`: false, `/$Recycle.Bin`: true,
	}
	for p, want := range cases {
		if got := isSkippedDir(p, p[len(p)-len(lastName(p)):]); got != want {
			t.Errorf("%s: got %v want %v", p, got, want)
		}
	}
}

func lastName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
