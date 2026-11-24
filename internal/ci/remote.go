package ci

import (
	"errors"
	"net/url"
	"strings"
)

// ParseRemote turns a git remote URL into a Repo. It accepts http, https,
// ssh and git URLs and scp-like "user@host:owner/name" remotes. Userinfo and
// port are dropped. Errors describe the problem without echoing the input,
// because a remote can embed credentials.
func ParseRemote(remote string) (Repo, error) {
	if remote == "" {
		return Repo{}, errors.New("empty remote")
	}
	if strings.TrimSpace(remote) != remote {
		return Repo{}, errors.New("remote has surrounding whitespace")
	}
	for i := 0; i < len(remote); i++ {
		if remote[i] < 0x20 || remote[i] == 0x7f {
			return Repo{}, errors.New("remote contains a control character")
		}
	}

	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			// url.Error embeds the input; do not wrap it.
			return Repo{}, errors.New("remote is not a valid URL")
		}
		switch u.Scheme {
		case "http", "https", "ssh", "git":
		default:
			return Repo{}, errors.New("unsupported remote scheme")
		}
		if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return Repo{}, errors.New("remote has a query or fragment")
		}
		host = u.Hostname()
		path = strings.TrimPrefix(u.Path, "/")
	} else {
		userHost, p, ok := strings.Cut(remote, ":")
		if !ok {
			return Repo{}, errors.New("remote is not a recognised URL or scp-like address")
		}
		user, h, ok := strings.Cut(userHost, "@")
		if !ok || user == "" {
			return Repo{}, errors.New("scp-like remote needs a user@host form")
		}
		host, path = h, p
	}

	if !validHost(host) {
		return Repo{}, errors.New("remote has a missing or invalid host")
	}
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || strings.Contains(name, "/") {
		return Repo{}, errors.New("remote path must be exactly owner/name")
	}
	if !validSegment(owner) || !validSegment(name) {
		return Repo{}, errors.New("remote owner or name is empty or has invalid characters")
	}
	return Repo{Host: strings.ToLower(host), Owner: owner, Name: name}, nil
}

func validHost(h string) bool {
	if h == "" {
		return false
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !isNameChar(c) || c == '_' {
			return false
		}
	}
	return true
}

// validSegment reports whether s matches ^[A-Za-z0-9._-]+$ and is not "."
// or "..".
func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isNameChar(s[i]) {
			return false
		}
	}
	return true
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '.' || c == '_' || c == '-'
}
