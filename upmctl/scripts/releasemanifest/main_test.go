package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestGenerateAndVerify(t *testing.T) {
	root := t.TempDir()
	for path, contents := range map[string]string{
		"docs/upmctl/deployment-guide.md":              "deployment",
		"scripts/validate-test-environment.sh":         "#!/bin/sh\n",
		"skills/upmctl-environment/SKILL.md":           "---\nname: upmctl-environment\n---\n",
		"skills/upmctl-environment/agents/openai.yaml": "interface: {}\n",
	} {
		fullPath := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	value, err := expectedManifest(
		root, "0.1.0-rc1", "abc123-dirty", "2026-07-17T00:00:00Z",
		"linux", "amd64", "rocky9-e2e-candidate", "upmctl_0.1.0-rc1_linux_amd64.tar.gz",
	)
	if err != nil {
		t.Fatalf("expectedManifest() error = %v", err)
	}
	wantSupport := []string{
		"docs/upmctl/deployment-guide.md",
		"scripts/validate-test-environment.sh",
		"skills/upmctl-environment/SKILL.md",
		"skills/upmctl-environment/agents/openai.yaml",
	}
	if !reflect.DeepEqual(value.Files.Support, wantSupport) {
		t.Fatalf("support files = %#v, want %#v", value.Files.Support, wantSupport)
	}
	if value.Platform.ValidationTier != "rocky9-e2e-candidate" || value.Files.InternalChecksums != "SHA256SUMS" {
		t.Fatalf("unexpected manifest contract: %#v", value)
	}

	path := filepath.Join(root, "release-manifest.json")
	if err := writeManifest(path, value); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}
	if err := verifyManifest(path, value); err != nil {
		t.Fatalf("verifyManifest() error = %v", err)
	}

	mismatch := value
	mismatch.Platform.ValidationTier = "experimental-build-only"
	if err := verifyManifest(path, mismatch); err == nil {
		t.Fatal("verifyManifest() accepted mismatched validation tier")
	}
}

func TestSupportFilesRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"docs", "scripts", "skills"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "docs"), filepath.Join(root, "scripts", "docs-link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := supportFiles(root); err == nil {
		t.Fatal("supportFiles() accepted a symlink")
	}
}
