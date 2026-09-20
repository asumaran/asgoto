package main

// What a checkout says about its remote, read straight from the filesystem
// with no subprocess: scanning dozens of repos at startup stays in the low
// milliseconds.
//
// This file is the same in every tool of the family that needs it.

import (
	"os"
	"path/filepath"
	"strings"
)

// resolveGitDir returns the git dir of the checkout at path, handling both
// main checkouts (.git dir) and linked worktrees (.git file with a "gitdir:"
// pointer). Returns "" for non-repos.
func resolveGitDir(path string) string {
	if path == "" {
		return ""
	}
	gitdir := filepath.Join(path, ".git")
	if fi, err := os.Stat(gitdir); err != nil {
		return ""
	} else if !fi.IsDir() {
		data, err := os.ReadFile(gitdir)
		if err != nil {
			return ""
		}
		target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
		if target == "" {
			return ""
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(path, target)
		}
		gitdir = target
	}
	return gitdir
}

// githubSlug resolves "owner/repo" from the origin remote of the repo at
// repoRoot, reading the git config file directly. Returns "" when the origin
// is missing or not on github.com.
func githubSlug(repoRoot string) string {
	gitdir := resolveGitDir(repoRoot)
	if gitdir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(gitdir, "config"))
	if err != nil {
		// A linked-worktree gitdir keeps config in the common dir.
		common, cerr := os.ReadFile(filepath.Join(gitdir, "commondir"))
		if cerr != nil {
			return ""
		}
		target := strings.TrimSpace(string(common))
		if !filepath.IsAbs(target) {
			target = filepath.Join(gitdir, target)
		}
		if data, err = os.ReadFile(filepath.Join(target, "config")); err != nil {
			return ""
		}
	}
	return githubSlugFromURL(originURL(string(data)))
}

// originURL scans git config content for the url of [remote "origin"].
func originURL(config string) string {
	inOrigin := false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "url"); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "="); ok {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// githubSlugFromURL extracts "owner/repo" from the ssh/https GitHub remote
// URL forms. Non-GitHub hosts return "".
func githubSlugFromURL(url string) string {
	var rest string
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		rest = strings.TrimPrefix(url, "git@github.com:")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		rest = strings.TrimPrefix(url, "ssh://git@github.com/")
	case strings.HasPrefix(url, "https://github.com/"):
		rest = strings.TrimPrefix(url, "https://github.com/")
	default:
		return ""
	}
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if strings.Count(rest, "/") != 1 {
		return ""
	}
	return rest
}
