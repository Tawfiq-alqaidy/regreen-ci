package ci

import (
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	gh := func(o, n string) Repo { return Repo{Host: "github.com", Owner: o, Name: n} }
	tests := []struct {
		name string
		in   string
		want Repo
	}{
		{"https with .git", "https://github.com/Tawfiq-alqaidy/regreen-ci.git", gh("Tawfiq-alqaidy", "regreen-ci")},
		{"https plain", "https://github.com/o/r", gh("o", "r")},
		{"https trailing slash", "https://github.com/o/r/", gh("o", "r")},
		{"https .git and slash", "https://github.com/o/r.git/", gh("o", "r")},
		{"http", "http://github.com/o/r", gh("o", "r")},
		{"git scheme", "git://github.com/o/r.git", gh("o", "r")},
		{"scp-like", "git@github.com:o/r.git", gh("o", "r")},
		{"scp-like no suffix", "git@GitHub.com:o/r", gh("o", "r")},
		{"ssh with port and upper host", "ssh://git@GitHub.com:22/o/r.git", gh("o", "r")},
		{"userinfo dropped", "https://user:secret@github.com/o/r.git", gh("o", "r")},
		{"https port dropped", "https://github.com:8443/o/r", gh("o", "r")},
		{"case kept", "https://github.com/OwNer/NaMe", gh("OwNer", "NaMe")},
		{"dots and underscores", "https://github.com/a.b_c/d-e.f", gh("a.b_c", "d-e.f")},
		{"name ending in .git.git", "https://github.com/o/r.git.git", gh("o", "r.git")},
		{"scp-like user with dot, dash and underscore", "my.git_user-1@github.com:o/r.git", gh("o", "r")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemote(tt.in)
			if err != nil {
				t.Fatalf("ParseRemote(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseRemote(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRemoteErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"owner only with credentials", "https://user:secret@github.com/o"},
		{"extra segment", "https://github.com/o/r/extra"},
		{"empty owner", "https://github.com//r"},
		{"scp missing owner", "git@github.com:r.git"},
		{"scp absolute path", "git@github.com:/o/r.git"},
		{"scp extra segment", "git@github.com:o/r/x.git"},
		{"local absolute path", "/srv/git/r.git"},
		{"local relative path", "../repo"},
		{"file scheme", "file:///srv/r.git"},
		{"ftp scheme", "ftp://h/o/r"},
		{"empty", ""},
		{"leading space", " https://github.com/o/r"},
		{"trailing newline", "https://github.com/o/r\n"},
		{"embedded control", "https://github.com/o/r\x00"},
		{"dotdot name", "https://github.com/o/.."},
		{"dot name", "https://github.com/o/."},
		{"space in name", "https://github.com/o/a b"},
		{"bad char in owner", "https://github.com/o%20x/r"},
		{"encoded slash", "https://github.com/o%2Fx/r"},
		{"query", "https://github.com/o/r?x=1"},
		{"fragment", "https://github.com/o/r#x"},
		{"empty host", "https:///o/r"},
		{"name is only .git", "https://github.com/o/.git"},
		{"scp empty host", "git@:o/r"},
		{"scp empty user", "@github.com:o/r"},
		{"bad port", "ssh://git@github.com:xx/o/r"},
		{"trailing-dot host", "https://github.com./o/r"},
		{"scp trailing-dot host", "git@github.com.:o/r"},
		{"leading-dot host", "https://.github.com/o/r"},
		{"double dot in host", "https://github..com/o/r"},
		{"dot-only host", "https://./o/r"},
		{"scp dot-only host", "git@..:o/r"},
		{"ipv6 host", "https://[::1]/o/r"},
		{"ipv6 host with port", "ssh://git@[2001:db8::1]:22/o/r"},
		{"scp without user", "github.com:o/r"},
		{"scp user with a space", "g it@github.com:o/r"},
		{"scp user with a slash", "a/b@github.com:o/r"},
		{"scp user with a percent", "git%40x@github.com:o/r"},
		{"scp user with a plus", "git+ssh@github.com:o/r"},
		{"scp user with a second at", "a@b@github.com:o/r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemote(tt.in)
			if err == nil {
				t.Fatalf("ParseRemote(%q) = %+v, want error", tt.in, got)
			}
			if got != (Repo{}) {
				t.Errorf("ParseRemote(%q) returned non-zero Repo %+v with an error", tt.in, got)
			}
		})
	}
}

func TestParseRemoteErrorsDoNotEchoInput(t *testing.T) {
	inputs := []string{
		"https://user:secret@github.com/o",
		"https://user:secret@github.com/o/r/extra",
		"https://user:secret@github.com:xx/o/r",
		"ftp://user:secret@h/o/r",
		"https://user:secret@github.com/o/a b",
		"user:secret@github.com:r",
		"https://user:secret@github.com/o/r?t=secret",
		" https://user:secret@github.com/o/r",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			_, err := ParseRemote(in)
			if err == nil {
				t.Fatal("want error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("error leaks credentials: %v", err)
			}
		})
	}
}
