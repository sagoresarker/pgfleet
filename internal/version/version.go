// Package version exposes the build version of the PgFleet control plane.
package version

// version is the current PgFleet version. It is overridable at build time via
// -ldflags "-X github.com/sagoresarker/pgfleet/internal/version.version=..."
var version = "0.1.0-dev"

// String returns the current PgFleet version.
func String() string {
	return version
}
