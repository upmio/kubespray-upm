package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/controlstate"
)

func TestApprovalStoreSaveReadGetAndList(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	createdAt := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	firstPlan := mustWorkerStartPlanFor(t, createdAt, "env-store-z", testDigest)
	secondPlan := mustWorkerStartPlanFor(t, createdAt, "env-store-a", testDigest)
	first := mustApproval(t, firstPlan, createdAt.Add(time.Minute))
	second := mustApproval(t, secondPlan, createdAt.Add(time.Minute))

	firstPath, err := store.Save(first)
	if err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	wantPath := filepath.Join(workspace, ".upmctl", "approvals", "by-plan", first.PlanID+".json")
	if firstPath != wantPath {
		t.Fatalf("Save(first) path = %q, want %q", firstPath, wantPath)
	}
	if _, err := store.Save(second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(workspace, ".upmctl"),
		filepath.Join(workspace, ".upmctl", "approvals"),
		filepath.Join(workspace, ".upmctl", "approvals", "by-plan"),
	} {
		approvalAssertMode(t, path, 0o700)
	}
	approvalAssertMode(t, firstPath, 0o600)

	read, err := store.ReadByPlan(first.PlanID)
	if err != nil {
		t.Fatalf("ReadByPlan() error = %v", err)
	}
	if !reflect.DeepEqual(read, first) {
		t.Fatalf("ReadByPlan() = %#v, want %#v", read, first)
	}
	got, err := store.GetByApprovalID(second.ApprovalID)
	if err != nil {
		t.Fatalf("GetByApprovalID() error = %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("GetByApprovalID() = %#v, want %#v", got, second)
	}

	listed, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []Approval{first, second}
	sort.Slice(want, func(i, j int) bool { return want[i].PlanID < want[j].PlanID })
	if !reflect.DeepEqual(listed, want) {
		t.Fatalf("List() = %#v, want %#v", listed, want)
	}
	filtered, err := store.List(first.PlanID)
	if err != nil {
		t.Fatalf("List(planID) error = %v", err)
	}
	if !reflect.DeepEqual(filtered, []Approval{first}) {
		t.Fatalf("List(planID) = %#v, want first Approval", filtered)
	}
}

func TestApprovalStoreMissingIsDistinctFromUnsafe(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	missingPlanID := "plan-" + strings.Repeat("a", 64)
	missingApprovalID := "approval-" + strings.Repeat("b", 64)

	if _, err := store.ReadByPlan(missingPlanID); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("ReadByPlan(missing) error = %v, want ErrApprovalNotFound", err)
	}
	listed, err := store.List()
	if err != nil || len(listed) != 0 {
		t.Fatalf("List(empty) = %v, %v, want empty success", listed, err)
	}
	if _, err := store.GetByApprovalID(missingApprovalID); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("GetByApprovalID(missing) error = %v, want ErrApprovalNotFound", err)
	}

	if err := os.Symlink(t.TempDir(), filepath.Join(workspace, ".upmctl")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadByPlan(missingPlanID); !errors.Is(err, controlstate.ErrUnsafe) {
		t.Fatalf("ReadByPlan(symlink) error = %v, want controlstate.ErrUnsafe", err)
	}
}

func TestApprovalStoreCollisionAndConcurrentSaveNeverOverwrite(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	createdAt := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	value := mustApproval(t, mustWorkerStartPlan(t, createdAt), createdAt.Add(time.Minute))

	start := make(chan struct{})
	errorsByCall := make([]error, 8)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = store.Save(value)
		}(index)
	}
	close(start)
	wait.Wait()

	succeeded, collided := 0, 0
	for _, err := range errorsByCall {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrApprovalExists):
			collided++
		default:
			t.Fatalf("concurrent Save() unexpected error = %v", err)
		}
	}
	if succeeded != 1 || collided != len(errorsByCall)-1 {
		t.Fatalf("concurrent Save results succeeded=%d collided=%d", succeeded, collided)
	}
	if got, err := store.ReadByPlan(value.PlanID); err != nil || !reflect.DeepEqual(got, value) {
		t.Fatalf("ReadByPlan() after collision = %#v, %v", got, err)
	}
}

func TestApprovalStoreReadUsesStrictJSONAndStorageBinding(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC)
	value := mustApproval(t, mustWorkerStartPlan(t, createdAt), createdAt.Add(time.Minute))
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		key    string
	}{
		{
			name: "unknown field",
			mutate: func(source []byte) []byte {
				return append(bytes.TrimSuffix(source, []byte("}")), []byte(`,"unknown":true}`)...)
			},
			key: value.PlanID,
		},
		{
			name: "duplicate nested field",
			mutate: func(source []byte) []byte {
				needle := []byte(`"username":"` + value.Approver.Username + `"`)
				replacement := append(append([]byte{}, needle...), append([]byte(","), needle...)...)
				return bytes.Replace(source, needle, replacement, 1)
			},
			key: value.PlanID,
		},
		{
			name:   "trailing value",
			mutate: func(source []byte) []byte { return append(source, []byte(`{}`)...) },
			key:    value.PlanID,
		},
		{
			name:   "storage key mismatch",
			mutate: func(source []byte) []byte { return source },
			key:    "plan-" + strings.Repeat("f", 64),
		},
		{
			name: "tampered semantics",
			mutate: func(source []byte) []byte {
				return bytes.Replace(source, []byte(value.Reason), []byte("tampered approval reason"), 1)
			},
			key: value.PlanID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			approvalWriteStored(t, workspace, test.key, test.mutate(append([]byte{}, encoded...)))
			if _, err := NewStore(workspace).ReadByPlan(test.key); err == nil {
				t.Fatal("ReadByPlan() error = nil, want strict rejection")
			}
		})
	}
}

func TestApprovalStoreRejectsInvalidIdentifiersBeforeStateAccess(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	if _, err := store.ReadByPlan("../bad"); !errors.Is(err, ErrInvalidPlanID) {
		t.Fatalf("ReadByPlan(invalid) error = %v, want ErrInvalidPlanID", err)
	}
	if _, err := store.GetByApprovalID("approval-bad"); !errors.Is(err, ErrInvalidApprovalID) {
		t.Fatalf("GetByApprovalID(invalid) error = %v, want ErrInvalidApprovalID", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid identifiers accessed control state: %v", err)
	}
}

func approvalWriteStored(t *testing.T, workspace, planID string, contents []byte) {
	t.Helper()
	directory := filepath.Join(workspace, ".upmctl", "approvals", "by-plan")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for path := directory; path != workspace; path = filepath.Dir(path) {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, planID+".json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func approvalAssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
