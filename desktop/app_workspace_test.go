package main

import "testing"

func TestPullRequestURL(t *testing.T) {
	cases := []struct {
		remote, want string
	}{
		{"https://github.com/acme/app.git", "https://github.com/acme/app/compare/main...feat/x?expand=1"},
		{"git@github.com:acme/app.git", "https://github.com/acme/app/compare/main...feat/x?expand=1"},
		{"https://gitlab.com/group/sub/app.git", "https://gitlab.com/group/sub/app/-/merge_requests/new?merge_request%5Bsource_branch%5D=feat%2Fx&merge_request%5Btarget_branch%5D=main"},
	}
	for _, c := range cases {
		got, err := pullRequestURL(c.remote, "main", "feat/x")
		if err != nil || got != c.want {
			t.Errorf("%s: got %q err %v, want %q", c.remote, got, err, c.want)
		}
	}
}
