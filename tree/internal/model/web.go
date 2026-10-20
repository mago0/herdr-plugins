package model

import (
	"net/url"
	"strings"
)

// segments is the path of link split at each slash, when link is a web address on host.
func segments(link, host string) []string {
	u, err := url.Parse(link)
	if err != nil || u.Host != host {
		return nil
	}
	return strings.Split(strings.Trim(u.Path, "/"), "/")
}

// IsProject is true for the address of a Linear project, as opposed to that of an issue.
func IsProject(link string) bool {
	p := segments(link, "linear.app")
	return len(p) >= 3 && p[1] == "project"
}

// PRNumber is "#<n>" for the address of a GitHub pull request, else "".
func PRNumber(link string) string {
	p := segments(link, "github.com")
	if len(p) >= 4 && p[2] == "pull" && p[3] != "" {
		return "#" + p[3]
	}
	return ""
}
