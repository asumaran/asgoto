//go:build !darwin

package main

import (
	"os"
	"time"
)

// birthTime has nothing to report here: stat(2) carries no creation time on
// Linux (statx does, behind a dependency this tool does not take), so the
// caller falls back to the modification time.
func birthTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
