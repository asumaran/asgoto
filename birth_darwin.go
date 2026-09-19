package main

import (
	"os"
	"syscall"
	"time"
)

// birthTime is the creation time of the file, which darwin's stat reports.
func birthTime(fi os.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), true
}
