package provision

import (
	"strings"
	"testing"
	"time"
)

// TestRedactSecretsTerminates guards against an infinite loop: the replacement
// "repo1-s3-key=***" still contains the search marker "repo1-s3-key=", so a
// naive in-place replace re-finds it forever, hanging every provision error
// path. It must terminate and scrub every secret value.
func TestRedactSecretsTerminates(t *testing.T) {
	in := "repo1-s3-key=AKIAEXAMPLE\nrepo1-s3-key-secret=SUPERSECRET\nrepo1-cipher-pass=hunter2\n"
	done := make(chan string, 1)
	go func() { done <- redactSecrets(in) }()

	select {
	case got := <-done:
		for _, leaked := range []string{"AKIAEXAMPLE", "SUPERSECRET", "hunter2"} {
			if strings.Contains(got, leaked) {
				t.Errorf("secret %q not redacted in %q", leaked, got)
			}
		}
		for _, want := range []string{"repo1-s3-key=***", "repo1-s3-key-secret=***", "repo1-cipher-pass=***"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in redacted output %q", want, got)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("redactSecrets did not terminate (infinite loop)")
	}
}

// TestRedactSecretsMultipleOccurrences — every occurrence of a key must be
// scrubbed, not just the first.
func TestRedactSecretsMultipleOccurrences(t *testing.T) {
	in := "repo1-s3-key=AAA and later repo1-s3-key=BBB"
	got := redactSecrets(in)
	if strings.Contains(got, "AAA") || strings.Contains(got, "BBB") {
		t.Errorf("not all occurrences redacted: %q", got)
	}
}

// TestRedactSecretsNoSecret — output without secrets is returned unchanged.
func TestRedactSecretsNoSecret(t *testing.T) {
	in := "pg_isready: accepting connections\nstanza-create ok"
	if got := redactSecrets(in); got != in {
		t.Errorf("non-secret output changed: %q -> %q", in, got)
	}
}
