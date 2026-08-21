package buildinfo

import (
	"os"
	"strings"
)

var (
	Version  = "dev"
	Revision = "unknown"

	versionFilePath = "/app/VERSION"
)

// RuntimeVersion returns the version packaged with the running image.
func RuntimeVersion() string {
	contents, err := os.ReadFile(versionFilePath)
	if err != nil {
		return Version
	}
	if version := strings.TrimSpace(string(contents)); version != "" {
		return version
	}
	return Version
}
