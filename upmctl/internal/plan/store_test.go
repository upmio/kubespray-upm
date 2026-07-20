package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const storeTestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestStoreSaveAndRead(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	want := validStoredPlan(t)

	path, err := store.Save(want)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	wantPath := filepath.Join(workspace, ".upmctl", "plans", want.PlanID+".json")
	if path != wantPath {
		t.Fatalf("Save() path = %q, want %q", path, wantPath)
	}
	assertMode(t, filepath.Join(workspace, ".upmctl"), storeDirectoryMode)
	assertMode(t, filepath.Join(workspace, ".upmctl", "plans"), storeDirectoryMode)
	assertMode(t, path, storeFileMode)

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("plan directory entries = %v, want only %s", entryNames(entries), filepath.Base(path))
	}

	got, err := store.Read(want.PlanID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Read() = %#v, want %#v", got, want)
	}
}

func TestStoreSaveRejectsNonActionablePlanWithoutCreatingState(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	plan := validStoredPlan(t)
	plan.Disposition = DispositionNoop

	if _, err := store.Save(plan); err == nil || !strings.Contains(err.Error(), "not ACTION_REQUIRED") {
		t.Fatalf("Save() error = %v, want non-actionable rejection", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-actionable Save created .upmctl or returned unexpected error: %v", err)
	}
}

func TestStoreSaveCollisionNeverOverwrites(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	first := validStoredPlan(t)
	path, err := store.Save(first)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.Save(first); !errors.Is(err, ErrPlanExists) {
		t.Fatalf("second Save() error = %v, want ErrPlanExists", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("plan collision changed the existing immutable plan")
	}
}

func TestStoreConcurrentSavePublishesExactlyOnce(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	plan := validStoredPlan(t)

	start := make(chan struct{})
	errorsByCall := make([]error, 2)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = store.Save(plan)
		}()
	}
	close(start)
	wait.Wait()

	succeeded, collided := 0, 0
	for _, err := range errorsByCall {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrPlanExists):
			collided++
		default:
			t.Fatalf("concurrent Save() unexpected error = %v", err)
		}
	}
	if succeeded != 1 || collided != 1 {
		t.Fatalf("concurrent Save results: succeeded=%d collided=%d", succeeded, collided)
	}
	if _, err := store.Read(plan.PlanID); err != nil {
		t.Fatalf("Read() after concurrent Save error = %v", err)
	}
}

func TestStoreRejectsInvalidPlanIDsBeforeFilesystemAccess(t *testing.T) {
	invalidIDs := []string{
		"",
		"plan-a",
		"plan-" + strings.Repeat("A", 64),
		"plan-" + strings.Repeat("a", 63),
		"plan-" + strings.Repeat("a", 65),
		"../plan-" + strings.Repeat("a", 64),
		"plan-" + strings.Repeat("a", 63) + "/",
	}
	for _, planID := range invalidIDs {
		t.Run(strings.ReplaceAll(planID, "/", "slash"), func(t *testing.T) {
			workspace := t.TempDir()
			store := NewStore(workspace)
			plan := validStoredPlan(t)
			plan.PlanID = planID
			if _, err := store.Save(plan); !errors.Is(err, ErrInvalidPlanID) {
				t.Fatalf("Save(%q) error = %v, want ErrInvalidPlanID", planID, err)
			}
			if _, err := store.Read(planID); !errors.Is(err, ErrInvalidPlanID) {
				t.Fatalf("Read(%q) error = %v, want ErrInvalidPlanID", planID, err)
			}
			if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid ID accessed store state: %v", err)
			}
		})
	}
}

func TestStoreReadMissingControlDirectoryReturnsNotExist(t *testing.T) {
	planID := validStoredPlan(t).PlanID
	tests := []struct {
		name    string
		prepare func(t *testing.T, workspace string)
	}{
		{
			name: "missing .upmctl",
			prepare: func(_ *testing.T, _ string) {
			},
		},
		{
			name: "missing plans",
			prepare: func(t *testing.T, workspace string) {
				if err := os.Mkdir(filepath.Join(workspace, ".upmctl"), storeDirectoryMode); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			test.prepare(t, workspace)

			_, err := NewStore(workspace).Read(planID)
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("Read() error = %v, want os.ErrNotExist", err)
			}
			if errors.Is(err, ErrUnsafeStore) {
				t.Fatalf("Read() error = %v, must not report ErrUnsafeStore for an absent store", err)
			}
			if _, statErr := os.Lstat(filepath.Join(workspace, ".upmctl", "plans")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("Read() created or changed the absent plan directory: %v", statErr)
			}
		})
	}
}

func TestStoreReadMissingDirectoryDoesNotMaskUnsafeStore(t *testing.T) {
	planID := validStoredPlan(t).PlanID

	t.Run("plans symlink", func(t *testing.T) {
		workspace := t.TempDir()
		upmctlDirectory := filepath.Join(workspace, ".upmctl")
		if err := os.Mkdir(upmctlDirectory, storeDirectoryMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(upmctlDirectory, "plans")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(workspace).Read(planID); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Read() error = %v, want ErrUnsafeStore", err)
		}
	})

	t.Run("plans permissions", func(t *testing.T) {
		workspace := t.TempDir()
		plansDirectory := createStoreDirectories(t, workspace)
		if err := os.Chmod(plansDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(workspace).Read(planID); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Read() error = %v, want ErrUnsafeStore", err)
		}
	})
}

func TestStoreRejectsSymlinkedControlDirectories(t *testing.T) {
	t.Run(".upmctl", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, ".upmctl")); err != nil {
			t.Fatal(err)
		}
		_, err := NewStore(workspace).Save(validStoredPlan(t))
		if !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Save() error = %v, want ErrUnsafeStore", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "plans")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Save wrote through .upmctl symlink: %v", err)
		}
	})

	t.Run("plans", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		upmctlDirectory := filepath.Join(workspace, ".upmctl")
		if err := os.Mkdir(upmctlDirectory, storeDirectoryMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(upmctlDirectory, "plans")); err != nil {
			t.Fatal(err)
		}
		plan := validStoredPlan(t)
		_, err := NewStore(workspace).Save(plan)
		if !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Save() error = %v, want ErrUnsafeStore", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, plan.PlanID+".json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Save wrote through plans symlink: %v", err)
		}
	})
}

func TestStoreReadUsesStrictJSON(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, plan Plan, encoded []byte) []byte
	}{
		{
			name: "unknown field",
			mutate: func(t *testing.T, _ Plan, encoded []byte) []byte {
				return append(bytes.TrimSuffix(encoded, []byte("}")), []byte(",\"unknown\":true}")...)
			},
		},
		{
			name: "duplicate field",
			mutate: func(t *testing.T, plan Plan, encoded []byte) []byte {
				needle := []byte(`"planId":"` + plan.PlanID + `"`)
				replacement := []byte(`"planId":"` + plan.PlanID + `","planId":"` + plan.PlanID + `"`)
				if !bytes.Contains(encoded, needle) {
					t.Fatalf("encoded plan does not contain %s", needle)
				}
				return bytes.Replace(encoded, needle, replacement, 1)
			},
		},
		{
			name: "trailing value",
			mutate: func(_ *testing.T, _ Plan, encoded []byte) []byte {
				return append(encoded, []byte("{}")...)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			plan := validStoredPlan(t)
			encoded, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			writeStoredPlan(t, workspace, plan.PlanID, test.mutate(t, plan, encoded), storeFileMode)
			if _, err := NewStore(workspace).Read(plan.PlanID); err == nil {
				t.Fatal("Read() error = nil, want strict JSON rejection")
			}
		})
	}
}

func TestStoreReadRejectsUnsafeOrMismatchedFile(t *testing.T) {
	t.Run("plan ID mismatch", func(t *testing.T) {
		workspace := t.TempDir()
		plan := validStoredPlan(t)
		requestedID := "plan-" + strings.Repeat("b", 64)
		encoded, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		writeStoredPlan(t, workspace, requestedID, encoded, storeFileMode)
		if _, err := NewStore(workspace).Read(requestedID); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("Read() error = %v, want ID mismatch", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		workspace := t.TempDir()
		plan := validStoredPlan(t)
		encoded, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(t.TempDir(), "outside.json")
		if err := os.WriteFile(outside, encoded, storeFileMode); err != nil {
			t.Fatal(err)
		}
		plansDirectory := createStoreDirectories(t, workspace)
		if err := os.Symlink(outside, filepath.Join(plansDirectory, plan.PlanID+".json")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(workspace).Read(plan.PlanID); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Read() error = %v, want ErrUnsafeStore", err)
		}
	})

	t.Run("permissions", func(t *testing.T) {
		workspace := t.TempDir()
		plan := validStoredPlan(t)
		encoded, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		writeStoredPlan(t, workspace, plan.PlanID, encoded, 0o644)
		if _, err := NewStore(workspace).Read(plan.PlanID); !errors.Is(err, ErrUnsafeStore) {
			t.Fatalf("Read() error = %v, want ErrUnsafeStore", err)
		}
	})
}

func validStoredPlan(t *testing.T) Plan {
	t.Helper()
	createdAt := time.Date(2026, 7, 17, 4, 0, 0, 0, time.UTC)
	plan := Plan{
		APIVersion:    APIVersion,
		Kind:          Kind,
		EnvironmentID: "env-store-test",
		Action:        ActionVMStart,
		Disposition:   DispositionActionRequired,
		CreatedAt:     createdAt.Format(time.RFC3339Nano),
		ExpiresAt:     createdAt.Add(DefaultTTL).Format(time.RFC3339Nano),
		RiskLevel:     "R1",
		Basis: Basis{
			ConfigDigest:        storeTestDigest,
			ManagedStateDigest:  storeTestDigest,
			ObservedStateDigest: storeTestDigest,
		},
		Target:            Target{Kind: "VirtualMachine", Name: "k8s-3"},
		AffectedResources: []string{"k8s-3"},
		Preconditions:     []string{"MANAGED_ENVIRONMENT_VALID"},
		ApprovalScope:     "vm.start:k8s-3",
		AcceptanceRefs:    []string{"AC-PLAN-002"},
		Steps: []Step{{
			ID:             "vm-start-01",
			Code:           "VAGRANT_UP_NO_PROVISION",
			Resource:       "k8s-3",
			Postconditions: []string{"LIBVIRT_RUNNING"},
			AcceptanceRefs: []string{"AC-PLAN-002"},
		}},
	}
	var err error
	plan.PlanDigest, err = plan.ExpectedDigest()
	if err != nil {
		t.Fatalf("ExpectedDigest() error = %v", err)
	}
	plan.PlanID, err = plan.ExpectedID()
	if err != nil {
		t.Fatalf("ExpectedID() error = %v", err)
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("test plan is invalid: %v", err)
	}
	return plan
}

func createStoreDirectories(t *testing.T, workspace string) string {
	t.Helper()
	upmctlDirectory := filepath.Join(workspace, ".upmctl")
	if err := os.Mkdir(upmctlDirectory, storeDirectoryMode); err != nil {
		t.Fatal(err)
	}
	plansDirectory := filepath.Join(upmctlDirectory, "plans")
	if err := os.Mkdir(plansDirectory, storeDirectoryMode); err != nil {
		t.Fatal(err)
	}
	return plansDirectory
}

func writeStoredPlan(t *testing.T, workspace, planID string, contents []byte, mode os.FileMode) {
	t.Helper()
	plansDirectory := createStoreDirectories(t, workspace)
	path := filepath.Join(plansDirectory, planID+".json")
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
