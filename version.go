package main

import (
	"regexp"
	"runtime/debug"
)

// pseudoVersion matches the version Go stamps on a build of an untagged
// commit, e.g. v1.0.1-0.20261126101500-0123456789ab(+dirty).
var pseudoVersion = regexp.MustCompile(`^(v\d+\.\d+\.\d+)-(?:[0-9A-Za-z.-]*\.)?(?:0\.)?\d{14}-([0-9a-f]{12})(\+dirty)?$`)

// version is gitview's release (a v-prefixed semver git tag), which Go reads
// from the module version or, for go build in a checkout, from git. A build
// past the latest tag shows as the next patch version, "-dev", and the
// commit, e.g. v1.0.1-dev+0123456; uncommitted changes add "-dirty".
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	return displayVersion(info.Main.Version)
}

func displayVersion(v string) string {
	if v == "" || v == "(devel)" {
		return "dev"
	}
	m := pseudoVersion.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	out := m[1] + "-dev+" + m[2][:7]
	if m[3] != "" {
		out += "-dirty"
	}
	return out
}
