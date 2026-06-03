package instance

import "testing"

func TestVersionSupported(t *testing.T) {
	for _, v := range []string{"13", "14", "15", "16", "17"} {
		if !VersionSupported(v) {
			t.Errorf("version %q should be supported", v)
		}
	}
	for _, v := range []string{"", "12", "18", "16.2", "latest", "x"} {
		if VersionSupported(v) {
			t.Errorf("version %q should NOT be supported", v)
		}
	}
}

func TestImageForVersion(t *testing.T) {
	if got := ImageForVersion("17"); got != "pgfleet/postgres-pgbackrest:17" {
		t.Errorf("ImageForVersion(17) = %q", got)
	}
	if DefaultImage != ImageForVersion(DefaultVersion) {
		t.Errorf("DefaultImage %q should equal ImageForVersion(DefaultVersion) %q", DefaultImage, ImageForVersion(DefaultVersion))
	}
}

func TestValidateRejectsUnsupportedVersion(t *testing.T) {
	base := NewInstance{Name: "orders-db", RepoType: RepoLocal, Password: "a-good-password"}

	// Empty version is allowed (defaulted later).
	if err := base.Validate(); err != nil {
		t.Errorf("empty version should validate: %v", err)
	}
	// Supported version is allowed.
	ok := base
	ok.PGVersion = "17"
	if err := ok.Validate(); err != nil {
		t.Errorf("version 17 should validate: %v", err)
	}
	// Unsupported version is rejected.
	bad := base
	bad.PGVersion = "12"
	if err := bad.Validate(); err == nil {
		t.Error("version 12 should be rejected")
	}
}
