package buildinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeVersionReadsPackagedVersionFile(t *testing.T) {
	originalPath := versionFilePath
	originalVersion := Version
	t.Cleanup(func() {
		versionFilePath = originalPath
		Version = originalVersion
	})

	versionFilePath = filepath.Join(t.TempDir(), "VERSION")
	Version = "fallback"
	if err := os.WriteFile(versionFilePath, []byte("0.4.49\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if actual := RuntimeVersion(); actual != "0.4.49" {
		t.Fatalf("RuntimeVersion() = %q, want %q", actual, "0.4.49")
	}
}

func TestRuntimeVersionFallsBackWhenVersionFileUnavailable(t *testing.T) {
	originalPath := versionFilePath
	originalVersion := Version
	t.Cleanup(func() {
		versionFilePath = originalPath
		Version = originalVersion
	})

	versionFilePath = filepath.Join(t.TempDir(), "missing")
	Version = "fallback"

	if actual := RuntimeVersion(); actual != "fallback" {
		t.Fatalf("RuntimeVersion() = %q, want %q", actual, "fallback")
	}
}
