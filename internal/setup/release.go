package setup

import (
	"fmt"
	"strings"
)

// The release links below are GitHub's plain web URLs, not the GitHub API:
// the API's unauthenticated rate limit (60 requests an hour per address)
// makes installs fail on shared or busy networks. This file has no OS
// dependency, so its tests run anywhere.

const (
	repoOwner = "SpikePy"
	repoName  = "Windows-StayWakeBlackScreen"
)

// latestPageURL redirects to the newest release's tag page, which is how
// Setup learns the version it is about to install.
func latestPageURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest", repoOwner, repoName)
}

// latestAssetURL redirects to asset in the newest release.
func latestAssetURL(asset string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/latest/download/%s", repoOwner, repoName, asset)
}

// tagFromLocation returns the release tag from the address latestPageURL
// redirects to, which ends in "/releases/tag/<tag>".
func tagFromLocation(location string) (string, error) {
	loc, _, _ := strings.Cut(location, "#")
	loc, _, _ = strings.Cut(loc, "?")
	loc = strings.TrimRight(loc, "/")
	const marker = "/releases/tag/"
	i := strings.LastIndex(loc, marker)
	if i < 0 {
		return "", fmt.Errorf("unexpected redirect to %q: not a release page", location)
	}
	tag := loc[i+len(marker):]
	if tag == "" || strings.Contains(tag, "/") {
		return "", fmt.Errorf("unexpected redirect to %q: no tag in it", location)
	}
	return tag, nil
}
