package managedenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
)

func TestAdoptLegacyLibvirtWorkspaceCreatesOnlyManagedIdentity(t *testing.T) {
	workspace := legacyWorkspace(t)
	before := snapshotTarget(t, workspace)
	state, err := Prepare(workspace, "env-delivery-lab")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	state = adoptedState(t, state)
	path, err := NewStore(workspace).Save(state)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if path != filepath.Join(state.Workspace, ".upmctl", "state.json") {
		t.Fatalf("Save() path = %q", path)
	}
	assertMode(t, filepath.Join(state.Workspace, ".upmctl"), 0o700)
	assertMode(t, path, 0o600)
	if len(state.Machines) != 5 || state.Files["Vagrantfile"] == "" || state.Files["vagrant/config.rb"] == "" {
		t.Fatalf("state = %#v", state)
	}
	deployment, err := upmcontext.Discover(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !deployment.Managed || deployment.Trust != upmcontext.TrustManagedValid || deployment.EnvironmentID != "env-delivery-lab" {
		t.Fatalf("deployment = %#v", deployment)
	}
	if after := snapshotTarget(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("adoption modified target files\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestPrepareBindsEveryPresentSupportedKubeconfig(t *testing.T) {
	workspace := legacyWorkspace(t)
	writeFixture(t, filepath.Join(workspace, "inventory", "sample", "artifacts", "admin.conf"), "first-kubeconfig\n", 0o600)
	writeFixture(t, filepath.Join(workspace, "artifacts", "admin.conf"), "second-kubeconfig\n", 0o600)
	state, err := Prepare(workspace, "env-kubeconfigs")
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"inventory/sample/artifacts/admin.conf", "artifacts/admin.conf"} {
		if state.Files[relative] == "" {
			t.Fatalf("kubeconfig %s was not bound", relative)
		}
	}
	state = adoptedState(t, state)
	if _, err := NewStore(workspace).Save(state); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(workspace, "artifacts", "admin.conf"), "drifted\n", 0o600)
	deployment, err := upmcontext.Discover(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if deployment.Trust != upmcontext.TrustInvalid {
		t.Fatalf("Trust = %s, want INVALID after bound kubeconfig drift", deployment.Trust)
	}
}

func TestPrepareRejectsUnsafeOrAmbiguousLegacyIdentity(t *testing.T) {
	tests := []struct {
		name string
		code FailureCode
		edit func(t *testing.T, workspace string)
	}{
		{name: "unsupported provider", code: FailureUnsupportedProvider, edit: func(t *testing.T, workspace string) {
			if err := os.Rename(filepath.Join(workspace, ".vagrant", "machines", "k8s-1", "libvirt"), filepath.Join(workspace, ".vagrant", "machines", "k8s-1", "parallels")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "multiple providers", code: FailureUnsupportedProvider, edit: func(t *testing.T, workspace string) {
			writeFixture(t, filepath.Join(workspace, ".vagrant", "machines", "k8s-1", "parallels", "id"), "parallels-id\n", 0o600)
		}},
		{name: "missing id", code: FailureMetadataInvalid, edit: func(t *testing.T, workspace string) {
			if err := os.Remove(filepath.Join(workspace, ".vagrant", "machines", "k8s-2", "libvirt", "id")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "invalid id", code: FailureMetadataInvalid, edit: func(t *testing.T, workspace string) {
			writeFixture(t, filepath.Join(workspace, ".vagrant", "machines", "k8s-2", "libvirt", "id"), "not-a-uuid\n", 0o600)
		}},
		{name: "duplicate id", code: FailureMetadataInvalid, edit: func(t *testing.T, workspace string) {
			contents, err := os.ReadFile(filepath.Join(workspace, ".vagrant", "machines", "k8s-1", "libvirt", "id"))
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, filepath.Join(workspace, ".vagrant", "machines", "k8s-2", "libvirt", "id"), string(contents), 0o600)
		}},
		{name: "symlink id", code: FailureMetadataInvalid, edit: func(t *testing.T, workspace string) {
			id := filepath.Join(workspace, ".vagrant", "machines", "k8s-2", "libvirt", "id")
			if err := os.Remove(id); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "id")
			writeFixture(t, outside, "22222222-2222-4222-8222-222222222222\n", 0o600)
			if err := os.Symlink(outside, id); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown node", code: FailureMetadataInvalid, edit: func(t *testing.T, workspace string) {
			writeFixture(t, filepath.Join(workspace, ".vagrant", "machines", "k8s-6", "libvirt", "id"), "66666666-6666-4666-8666-666666666666\n", 0o600)
		}},
		{name: "unsafe config", code: FailureConfigInvalid, edit: func(t *testing.T, workspace string) {
			path := filepath.Join(workspace, "vagrant", "config.rb")
			file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = file.WriteString("\nsystem(\"true\")\n")
			_ = file.Close()
		}},
		{name: "existing control state", code: FailureAlreadyControlled, edit: func(t *testing.T, workspace string) {
			writeFixture(t, filepath.Join(workspace, ".upmctl", "plans", "existing.json"), "{}\n", 0o600)
			if err := os.Chmod(filepath.Join(workspace, ".upmctl"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := legacyWorkspace(t)
			test.edit(t, workspace)
			_, err := Prepare(workspace, "env-reject-test")
			if err == nil || FailureOf(err).Code != test.code {
				t.Fatalf("Prepare() error = %v (%s), want %s", err, FailureOf(err).Code, test.code)
			}
		})
	}
}

func TestPrepareRejectsInvalidEnvironmentIDAndSymlinkWorkspace(t *testing.T) {
	workspace := legacyWorkspace(t)
	if _, err := Prepare(workspace, "Prod Cluster"); err == nil || FailureOf(err).Code != FailureInvalidEnvironmentID {
		t.Fatalf("invalid environment ID error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(workspace, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(link, "env-symlink"); err == nil || FailureOf(err).Code != FailureUnsafeWorkspace {
		t.Fatalf("symlink workspace error = %v", err)
	}
}

func TestConcurrentSavePublishesExactlyOneState(t *testing.T) {
	workspace := legacyWorkspace(t)
	state, err := Prepare(workspace, "env-concurrent")
	if err != nil {
		t.Fatal(err)
	}
	state = adoptedState(t, state)
	start := make(chan struct{})
	errorsByCall := make([]error, 12)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = NewStore(workspace).Save(state)
		}(index)
	}
	close(start)
	wait.Wait()
	succeeded := 0
	for _, err := range errorsByCall {
		if err == nil {
			succeeded++
			continue
		}
		code := FailureOf(err).Code
		if code != FailureStateExists && code != FailureAlreadyControlled {
			t.Fatalf("concurrent Save() error = %v (%s)", err, code)
		}
	}
	if succeeded != 1 {
		t.Fatalf("successful saves = %d, want 1", succeeded)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".upmctl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("control directory entries = %v", names(entries))
	}
}

func TestRollbackRemovesOnlyTheExactPublishedState(t *testing.T) {
	workspace := legacyWorkspace(t)
	prepared, err := Prepare(workspace, "env-rollback")
	if err != nil {
		t.Fatal(err)
	}
	state := adoptedState(t, prepared)
	store := NewStore(workspace)
	if _, err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := store.Rollback(state); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("exact rollback left control directory: %v", err)
	}

	prepared, err = Prepare(workspace, "env-rollback-replaced")
	if err != nil {
		t.Fatal(err)
	}
	state = adoptedState(t, prepared)
	if _, err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(state.Workspace, ".upmctl", "state.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Rollback(state); err == nil || FailureOf(err).Code != FailureStoreUnsafe {
		t.Fatalf("Rollback(replaced) error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "replacement\n" {
		t.Fatalf("rollback removed or changed replacement: %q err=%v", contents, err)
	}
}

func legacyWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	writeFixture(t, filepath.Join(workspace, "Vagrantfile"), "# immutable fixture Vagrantfile\n", 0o644)
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	configContents, err := os.ReadFile(filepath.Join(repository, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(workspace, "vagrant", "config.rb"), string(configContents), 0o600)
	for index := 1; index <= 5; index++ {
		uuid := fmt.Sprintf("%08d-1111-4111-8111-%012d\n", index, index)
		writeFixture(t, filepath.Join(workspace, ".vagrant", "machines", fmt.Sprintf("k8s-%d", index), "libvirt", "id"), uuid, 0o600)
	}
	return workspace
}

func adoptedState(t *testing.T, state State) State {
	t.Helper()
	value, err := BindAdoption(state, ActorObservation{
		Subject: "os-user:1000", UID: "1000", Username: "operator", Hostname: "test-host",
	}, PresenceObservation{
		Terminal: "/dev/tty", ChallengeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, "verified exact legacy workspace identities", "req-adopt-test", "0.1.0-test", time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func writeFixture(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func snapshotTarget(t *testing.T, workspace string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if relative == ".upmctl" {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(contents)
			value += ":" + hex.EncodeToString(digest[:])
		}
		result[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %04o, want %04o", path, info.Mode().Perm(), want)
	}
}

func names(entries []os.DirEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	sort.Strings(result)
	return result
}
