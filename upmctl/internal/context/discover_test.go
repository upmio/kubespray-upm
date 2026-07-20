package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverStandardNestedWorkspace(t *testing.T) {
	repository := t.TempDir()
	mustWrite(t, filepath.Join(repository, "vagrant_setup_scripts", "libvirt_kubespray_setup.sh"))
	mustWrite(t, filepath.Join(repository, "playbooks", "cluster.yml"))
	workspace := filepath.Join(repository, "vagrant_setup_scripts", "kubespray-upm")
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))

	deployment, err := Discover(repository, "")
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if deployment.RepositoryRoot != repository {
		t.Fatalf("RepositoryRoot = %q, want %q", deployment.RepositoryRoot, repository)
	}
	if deployment.Workspace != workspace {
		t.Fatalf("Workspace = %q, want %q", deployment.Workspace, workspace)
	}
	if deployment.Source != "standard-nested" {
		t.Fatalf("Source = %q, want standard-nested", deployment.Source)
	}
	if deployment.Managed {
		t.Fatal("legacy fixture must not be reported as managed")
	}
}

func TestDiscoverExplicitWorkspace(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))
	mustManagedState(t, workspace)

	deployment, err := Discover(t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if !deployment.Managed {
		t.Fatal("workspace with .upmctl/state.json must be managed")
	}
	if deployment.Source != "explicit" {
		t.Fatalf("Source = %q, want explicit", deployment.Source)
	}
	if deployment.Trust != TrustManagedValid {
		t.Fatalf("Trust = %q, want %q", deployment.Trust, TrustManagedValid)
	}
}

func TestDiscoverRejectsExplicitNonWorkspace(t *testing.T) {
	_, err := Discover(t.TempDir(), t.TempDir())
	if err == nil {
		t.Fatal("Discover() error = nil, want invalid explicit workspace error")
	}
}

func TestDiscoverInvalidManagedStateFailsClosed(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))
	mustWrite(t, filepath.Join(workspace, ".upmctl", "state.json"))

	deployment, err := Discover(workspace, workspace)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if deployment.Managed || deployment.Trust != TrustInvalid {
		t.Fatalf("deployment = %#v, want invalid unmanaged state", deployment)
	}
}

func TestDiscoverRejectsUnknownManagedStateField(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))
	mustManagedState(t, workspace)
	path := filepath.Join(workspace, ".upmctl", "state.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	state["unexpected"] = true
	contents, _ = json.Marshal(state)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	deployment, err := Discover(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Trust != TrustInvalid {
		t.Fatalf("Trust = %q, want INVALID", deployment.Trust)
	}
}

func TestDiscoverRejectsDuplicateManagedStateKey(t *testing.T) {
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))
	mustManagedState(t, workspace)
	path := filepath.Join(workspace, ".upmctl", "state.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	contents = append([]byte(`{"kind":"ManagedEnvironment",`), contents[1:]...)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	deployment, err := Discover(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Trust != TrustInvalid {
		t.Fatalf("Trust = %q, want INVALID", deployment.Trust)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func mustManagedState(t *testing.T, workspace string) {
	t.Helper()
	files := map[string]string{}
	for _, relative := range []string{"Vagrantfile", "vagrant/config.rb"} {
		contents, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		files[relative] = "sha256:" + hex.EncodeToString(digest[:])
	}
	state := map[string]any{
		"apiVersion":    "upmctl.upm.io/v1alpha1",
		"kind":          "ManagedEnvironment",
		"environmentId": "env-test",
		"workspace":     workspace,
		"files":         files,
		"adoption":      testAdoptionEvidence(),
	}
	contents, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, ".upmctl", "state.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testAdoptionEvidence() map[string]any {
	return map[string]any{
		"adoptedAt":     "2026-07-17T12:00:00Z",
		"actor":         map[string]any{"subject": "os-user:1000", "uid": "1000", "username": "operator", "hostname": "test-host", "source": "human-cli", "authMethod": "interactive-tty"},
		"humanPresence": map[string]any{"method": "typed-challenge", "terminal": "/dev/tty", "challengeDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "confirmedAt": "2026-07-17T12:00:00Z"},
		"reason":        "test fixture adoption", "requestId": "req-context-test", "cliVersion": "0.1.0-test",
	}
}
