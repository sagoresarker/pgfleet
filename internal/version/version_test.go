package version

import (
	"regexp"
	"testing"
)

func TestStringReturnsSemver(t *testing.T) {
	got := String()
	if got == "" {
		t.Fatal("version.String() returned empty string")
	}

	// Expect semantic-version-ish output, optionally with a build suffix.
	semver := regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	if !semver.MatchString(got) {
		t.Fatalf("version.String() = %q, want semver", got)
	}
}
