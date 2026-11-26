package main

import "testing"

func TestDisplayVersion(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "dev",
		"(devel)":                              "dev",
		"v1.0.0":                               "v1.0.0",
		"v1.2.3+dirty":                         "v1.2.3+dirty",
		"v1.0.1-0.20261126101500-0123456789ab": "v1.0.1-dev+0123456",
		"v1.0.1-0.20261126101500-0123456789ab+dirty": "v1.0.1-dev+0123456-dirty",
		"v0.0.0-20261126101500-0123456789ab":         "v0.0.0-dev+0123456",
		"v1.1.0-rc.1.0.20261126101500-0123456789ab":  "v1.1.0-dev+0123456",
	} {
		if got := displayVersion(in); got != want {
			t.Errorf("displayVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
