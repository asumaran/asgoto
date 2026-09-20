package main

import "testing"

func TestGithubSlugFromURL(t *testing.T) {
	cases := []struct{ url, want string }{
		{"git@github.com:masmovil/monorepo-front.git", "masmovil/monorepo-front"},
		{"git@github.com:asumaran/asgoto", "asumaran/asgoto"},
		{"ssh://git@github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"https://github.com/owner/repo/", "owner/repo"},
		{"git@gitlab.com:owner/repo.git", ""}, // non-GitHub host
		{"https://github.com/owner", ""},      // no repo segment
		{"", ""},
	}
	for _, c := range cases {
		if got := githubSlugFromURL(c.url); got != c.want {
			t.Errorf("githubSlugFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}

func TestOriginURL(t *testing.T) {
	config := `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:other/upstream.git
[remote "origin"]
	url = git@github.com:owner/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`
	if got := originURL(config); got != "git@github.com:owner/repo.git" {
		t.Errorf("originURL = %q, want origin url", got)
	}
	if got := originURL("[core]\n\tbare = false\n"); got != "" {
		t.Errorf("originURL without origin = %q, want empty", got)
	}
}
