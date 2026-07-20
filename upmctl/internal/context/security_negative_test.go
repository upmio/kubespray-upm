package context

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedTrustFailsClosedAcrossIdentityAndPermissionChanges(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, workspace string)
	}{
		{
			name: "managed state permissions widened",
			mutate: func(t *testing.T, workspace string) {
				if err := os.Chmod(filepath.Join(workspace, ".upmctl", "state.json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "control directory permissions widened",
			mutate: func(t *testing.T, workspace string) {
				if err := os.Chmod(filepath.Join(workspace, ".upmctl"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "managed state replaced by symlink",
			mutate: func(t *testing.T, workspace string) {
				path := filepath.Join(workspace, ".upmctl", "state.json")
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "state.json")
				if err := os.WriteFile(outside, contents, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "bound config replaced by symlink",
			mutate: func(t *testing.T, workspace string) {
				path := filepath.Join(workspace, "vagrant", "config.rb")
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "config.rb")
				if err := os.WriteFile(outside, contents, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "bound file digest drift",
			mutate: func(t *testing.T, workspace string) {
				path := filepath.Join(workspace, "Vagrantfile")
				if err := os.WriteFile(path, []byte("drifted fixture\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "duplicate machine UUID",
			mutate: func(t *testing.T, workspace string) {
				mutateManagedState(t, workspace, func(state map[string]any) {
					state["machines"] = map[string]any{
						"k8s-1": "11111111-1111-4111-8111-111111111111",
						"k8s-2": "11111111-1111-4111-8111-111111111111",
					}
				})
			},
		},
		{
			name: "oversized managed state",
			mutate: func(t *testing.T, workspace string) {
				path := filepath.Join(workspace, ".upmctl", "state.json")
				if err := os.WriteFile(path, []byte(strings.Repeat("x", (1<<20)+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := newManagedSecurityWorkspace(t)
			test.mutate(t, workspace)
			deployment, err := Discover(workspace, workspace)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if deployment.Managed || deployment.Trust != TrustInvalid {
				t.Fatalf("deployment = %#v, want fail-closed INVALID trust", deployment)
			}
		})
	}
}

func TestTrustTransitionManagedStateRemovalReturnsToLegacyReadOnly(t *testing.T) {
	workspace := newManagedSecurityWorkspace(t)
	managed, err := Discover(workspace, workspace)
	if err != nil || !managed.Managed || managed.Trust != TrustManagedValid {
		t.Fatalf("managed discovery = %#v, %v", managed, err)
	}
	if err := os.Remove(filepath.Join(workspace, ".upmctl", "state.json")); err != nil {
		t.Fatal(err)
	}
	legacy, err := Discover(workspace, workspace)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Managed || legacy.Trust != TrustLegacyReadOnly || legacy.EnvironmentID != "" {
		t.Fatalf("legacy discovery = %#v", legacy)
	}
}

func newManagedSecurityWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, "Vagrantfile"))
	mustWrite(t, filepath.Join(workspace, "vagrant", "config.rb"))
	mustManagedState(t, workspace)
	return workspace
}

func mutateManagedState(t *testing.T, workspace string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(workspace, ".upmctl", "state.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	mutate(state)
	contents, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
