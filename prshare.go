package main

// The shared PR cache: which PR each checkout's branch belongs to, and what
// each of those PRs is, in one file (prs.json in asmeta's state dir). asmeta
// is its only writer: one GitHub query per refresh for every branch of every
// workspace. The other tools only read it: asgoto shows the branch → PR
// association; asgotoissues and asgotopr, which find their PRs with their own
// searches, borrow the facts of a PR by its URL when the file saw a newer
// version of it (updated_at).
//
// Two maps, so a PR's identity never comes from a branch name alone:
//
//	branches[<head repo>][<branch>] = {url, checked_at}   url null: checked, no PR
//	pulls[<url>]                    = {repo, number, head, base, state, title, body, updated_at, checked_at}
//
// The head repo is the checkout's origin, "owner/repo" lowercased; a PR's
// repo is where it was opened (the upstream of a fork). state is normalized
// once: open | draft | merged | closed, draft only while open.
//
// This file is the same in every tool of the family that shows PRs.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	sharedPRVersion = 1
	// sharedPRKeep is how long an entry nobody checks any more is kept.
	sharedPRKeep = 30 * 24 * time.Hour
)

type sharedPR struct {
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	Head      string    `json:"head"`
	Base      string    `json:"base"`
	State     string    `json:"state"` // open | draft | merged | closed
	Title     string    `json:"title"`
	Body      string    `json:"body,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	CheckedAt time.Time `json:"checked_at"`
}

type sharedBranch struct {
	URL       *string   `json:"url"`
	CheckedAt time.Time `json:"checked_at"`
}

type sharedPRs struct {
	Version            int                                `json:"version"`
	LastQueryStartedAt time.Time                          `json:"last_query_started_at"`
	BackoffUntil       *time.Time                         `json:"backoff_until"`
	Branches           map[string]map[string]sharedBranch `json:"branches"`
	Pulls              map[string]sharedPR                `json:"pulls"`
}

// sharedPRFile is prs.json in asmeta's state dir, a sibling of this tool's
// own (stateDirFor resolves the same tree inside herdr and on its own).
func sharedPRFile() string {
	return filepath.Join(filepath.Dir(stateDirFor("asmeta")), pluginOwner+"asmeta", "prs.json")
}

func emptySharedPRs() sharedPRs {
	return sharedPRs{Version: sharedPRVersion, Branches: map[string]map[string]sharedBranch{}, Pulls: map[string]sharedPR{}}
}

// loadSharedPRs reads the file. A missing file, one that does not parse or
// has another version reads as empty, never as half a file.
func loadSharedPRs() sharedPRs {
	data, err := os.ReadFile(sharedPRFile())
	if err != nil {
		return emptySharedPRs()
	}
	return decodeSharedPRs(data)
}

func decodeSharedPRs(data []byte) sharedPRs {
	var s sharedPRs
	if json.Unmarshal(data, &s) != nil || s.Version != sharedPRVersion {
		return emptySharedPRs()
	}
	if s.Branches == nil {
		s.Branches = map[string]map[string]sharedBranch{}
	}
	if s.Pulls == nil {
		s.Pulls = map[string]sharedPR{}
	}
	return s
}

// branch is the PR of branch in the checkout whose origin is slug. checked is
// false when the branch was never checked; pr is nil when it was and has no
// PR. at is when it was checked.
func (s sharedPRs) branch(slug, branch string) (pr *sharedPR, checked bool, at time.Time) {
	b, ok := s.Branches[strings.ToLower(slug)][branch]
	if !ok {
		return nil, false, time.Time{}
	}
	if b.URL == nil {
		return nil, true, b.CheckedAt
	}
	p, ok := s.Pulls[*b.URL]
	if !ok {
		return nil, true, b.CheckedAt
	}
	return &p, true, b.CheckedAt
}

// pull is the PR at url.
func (s sharedPRs) pull(url string) (sharedPR, bool) {
	p, ok := s.Pulls[url]
	return p, ok
}

// newerThan: the file saw a later version of the PR than t.
func (p sharedPR) newerThan(t time.Time) bool { return p.UpdatedAt.After(t) }

// sharedPRState normalizes GitHub's state and draft flag.
func sharedPRState(state string, isDraft bool) string {
	switch strings.ToUpper(state) {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if isDraft {
		return "draft"
	}
	return "open"
}

// prSkipBranch: a branch whose PR is never looked up. A PR with that head is
// someone else's release train, and a detached head has no branch.
func prSkipBranch(b string) bool {
	switch b {
	case "", "main", "master", "develop":
		return true
	}
	return false
}

// ---- the writer's side (asmeta) ----

// sharedPRCheck is what one refresh learned about the branches of one head
// repo: branch → its PR's URL ("" = no PR), and the PRs themselves.
type sharedPRCheck struct {
	Branches map[string]string
	Pulls    map[string]sharedPR
}

// merge applies a refresh to s: every checked branch and PR is replaced, the
// rest kept, so a repo that failed this time, or one only another herdr
// session has, is never erased. Entries not checked for sharedPRKeep go.
func (s *sharedPRs) merge(checks map[string]sharedPRCheck, now time.Time) {
	for slug, c := range checks {
		slug = strings.ToLower(slug)
		if s.Branches[slug] == nil {
			s.Branches[slug] = map[string]sharedBranch{}
		}
		for b, url := range c.Branches {
			entry := sharedBranch{CheckedAt: now}
			if url != "" {
				u := url
				entry.URL = &u
			}
			s.Branches[slug][b] = entry
		}
		for url, p := range c.Pulls {
			p.CheckedAt = now
			s.Pulls[url] = p
		}
	}
	referenced := map[string]bool{}
	for slug, bs := range s.Branches {
		for b, e := range bs {
			if now.Sub(e.CheckedAt) > sharedPRKeep {
				delete(bs, b)
				continue
			}
			if e.URL != nil {
				referenced[*e.URL] = true
			}
		}
		if len(bs) == 0 {
			delete(s.Branches, slug)
		}
	}
	for url := range s.Pulls {
		if !referenced[url] {
			delete(s.Pulls, url)
		}
	}
}

// save writes s through a temporary file and a rename.
func (s sharedPRs) save() error {
	if s.Version != sharedPRVersion || s.Branches == nil || s.Pulls == nil {
		return errors.New("prs.json: refusing to write an incomplete document")
	}
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	writeFileAtomic(sharedPRFile(), data)
	return nil
}

// lockSharedPRs takes the writer's lock (prs.lock next to the file), waiting
// up to wait for another writer to finish. The kernel releases it when the
// process ends, however it ends.
func lockSharedPRs(wait time.Duration) (unlock func(), err error) {
	path := filepath.Join(filepath.Dir(sharedPRFile()), "prs.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return func() {
				syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, errors.New("prs.lock: another refresh is still running")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
