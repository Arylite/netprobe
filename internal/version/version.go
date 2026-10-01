package version

import "fmt"

// Set at link time: -ldflags "-X github.com/Arylite/netprobe/internal/version.Version=0.1.0".
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String describes the build on one line.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
