package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	upmplan "github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/readiness"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
)

type countingRunner struct {
	calls int
}

func (r *countingRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	r.calls++
	return runner.Result{}, nil
}

type planFixtureRunner struct {
	results    map[string]runner.Result
	commands   []runner.Command
	unexpected []runner.Command
}

func (r *planFixtureRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	key := command.Executable + " " + fmt.Sprint(command.Args)
	result, ok := r.results[key]
	if !ok {
		r.unexpected = append(r.unexpected, command)
		return runner.Result{}, fmt.Errorf("unexpected or non-read-only command: %s", key)
	}
	return result, nil
}

func TestListVMsRejectsUnsafeConfigBeforeExecutingWorkspace(t *testing.T) {
	workspace := managedWorkspace(t, true)
	commandRunner := &countingRunner{}
	_, appErr := New(commandRunner).ListVMs(context.Background(), workspace, workspace)
	if appErr == nil || appErr.Code != "UPMCTL_CONFIG_INVALID" {
		t.Fatalf("error = %#v, want UPMCTL_CONFIG_INVALID", appErr)
	}
	if commandRunner.calls != 0 {
		t.Fatalf("runner calls = %d, unsafe config must not execute external commands", commandRunner.calls)
	}
}

func TestPlanVMStartCreatesAuditableReadOnlyPlan(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	commandRunner := newPlanFixtureRunner(kubeconfig)
	now := time.Date(2026, 7, 17, 3, 4, 5, 0, time.UTC)

	created, appErr := New(commandRunner).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", now)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}
	if created.Disposition != "ACTION_REQUIRED" || created.RiskLevel != "R1" {
		t.Fatalf("plan disposition/risk = %s/%s, want ACTION_REQUIRED/R1", created.Disposition, created.RiskLevel)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, created.CreatedAt)
	if err != nil {
		t.Fatalf("createdAt = %q: %v", created.CreatedAt, err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, created.ExpiresAt)
	if err != nil {
		t.Fatalf("expiresAt = %q: %v", created.ExpiresAt, err)
	}
	if expiresAt.Sub(createdAt) != 30*time.Minute {
		t.Fatalf("plan TTL = %s, want 30m", expiresAt.Sub(createdAt))
	}

	plansDirectory := filepath.Join(workspace, ".upmctl", "plans")
	entries, err := os.ReadDir(plansDirectory)
	if err != nil {
		t.Fatalf("read plan store: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != created.PlanID+".json" {
		t.Fatalf("plan files = %#v, want only %s.json", entries, created.PlanID)
	}
	planPath := filepath.Join(plansDirectory, entries[0].Name())
	info, err := os.Lstat(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("plan file mode = %v/%04o, want regular/0600", info.Mode().Type(), info.Mode().Perm())
	}
	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoExecutionFields(t, contents)
	if len(commandRunner.unexpected) != 0 {
		t.Fatalf("runner received unexpected commands: %#v", commandRunner.unexpected)
	}
	assertReadOnlyCommands(t, commandRunner.commands)
}

func TestPhase2b1PlanAuditAndPreflightRemainReadOnly(t *testing.T) {
	workspace, kubeconfig := managedPlanWorkspace(t)
	now := time.Date(2026, 7, 17, 5, 6, 7, 0, time.UTC)
	created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", now)
	if appErr != nil {
		t.Fatalf("PlanVMStart() error = %#v", appErr)
	}
	if created.Disposition != upmplan.DispositionActionRequired {
		t.Fatalf("Disposition = %q, want ACTION_REQUIRED", created.Disposition)
	}

	beforeAudit := snapshotTree(t, workspace)
	auditRunner := &countingRunner{}
	auditService := New(auditRunner)
	inspection, appErr := auditService.GetPlan(workspace, workspace, created.PlanID, now.Add(5*time.Minute))
	if appErr != nil {
		t.Fatalf("GetPlan() error = %#v", appErr)
	}
	if inspection.Plan.PlanID != created.PlanID || inspection.Expired || inspection.ExecutionAvailable {
		t.Fatalf("inspection = %#v", inspection)
	}
	validation, appErr := auditService.ValidatePlan(workspace, workspace, created.PlanID, now.Add(5*time.Minute))
	if appErr != nil {
		t.Fatalf("ValidatePlan() error = %#v", appErr)
	}
	if validation.ArtifactStatus != readiness.StateValid || validation.FreshnessStatus != readiness.FreshnessCurrent ||
		validation.EnvironmentBinding != readiness.BindingMatch || validation.ConfigBinding != readiness.BindingMatch ||
		validation.ManagedStateBinding != readiness.BindingMatch || validation.ObservedStateBinding != readiness.BindingNotChecked ||
		validation.ExecutionAvailable || len(validation.Blockers) != 0 {
		t.Fatalf("validation = %#v", validation)
	}
	if auditRunner.calls != 0 {
		t.Fatalf("plan get/validate runner calls = %d, want 0", auditRunner.calls)
	}
	if afterAudit := snapshotTree(t, workspace); !reflect.DeepEqual(afterAudit, beforeAudit) {
		t.Fatalf("plan get/validate changed workspace\nbefore: %#v\nafter:  %#v", beforeAudit, afterAudit)
	}

	preflightRunner := newPlanFixtureRunner(kubeconfig)
	preflight, appErr := New(preflightRunner).PreflightPlan(context.Background(), workspace, workspace, created.PlanID, func() time.Time {
		return now.Add(10 * time.Minute)
	})
	if appErr != nil {
		t.Fatalf("PreflightPlan() error = %#v", appErr)
	}
	if preflight.PreflightStatus != readiness.PreflightPassed || preflight.ApplyDecision != readiness.ApplyDecisionBlocked ||
		preflight.ExecutionAvailable || preflight.ApprovalStatus != readiness.ApprovalMissing {
		t.Fatalf("preflight decision = %#v", preflight)
	}
	if preflight.Basis.Config.Status != readiness.BasisMatch || preflight.Basis.ManagedState.Status != readiness.BasisMatch || preflight.Basis.ObservedState.Status != readiness.BasisMatch {
		t.Fatalf("preflight basis = %#v, want all MATCH", preflight.Basis)
	}
	if len(preflightRunner.commands) == 0 || len(preflightRunner.unexpected) != 0 {
		t.Fatalf("preflight commands/unexpected = %#v/%#v", preflightRunner.commands, preflightRunner.unexpected)
	}
	assertReadOnlyCommands(t, preflightRunner.commands)
	if afterPreflight := snapshotTree(t, workspace); !reflect.DeepEqual(afterPreflight, beforeAudit) {
		t.Fatalf("preflight changed workspace\nbefore: %#v\nafter:  %#v", beforeAudit, afterPreflight)
	}
	assertPhase2b1StateAbsent(t, workspace)
}

func TestReadStoredPlanMapsMissingPlansDirectoryToNotFound(t *testing.T) {
	workspace := managedWorkspace(t, false)
	planID := "plan-" + strings.Repeat("a", 64)

	_, appErr := readStoredPlan(workspace, planID)
	if appErr == nil || appErr.Code != "UPMCTL_PLAN_NOT_FOUND" || appErr.ExitCode != 3 {
		t.Fatalf("readStoredPlan() error = %#v, want UPMCTL_PLAN_NOT_FOUND with exit code 3", appErr)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl", "plans")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("readStoredPlan() created or changed the absent plans directory: %v", err)
	}
}

func TestPhase2b1PreflightReportsIndividualBasisDrift(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*upmplan.Plan)
		basisState func(readiness.PreflightResult) string
		blocker    string
	}{
		{
			name: "config",
			mutate: func(candidate *upmplan.Plan) {
				candidate.Basis.ConfigDigest = alternateDigest(candidate.Basis.ConfigDigest)
			},
			basisState: func(result readiness.PreflightResult) string { return result.Basis.Config.Status },
			blocker:    readiness.CodeConfigDrift,
		},
		{
			name: "managed-state",
			mutate: func(candidate *upmplan.Plan) {
				candidate.Basis.ManagedStateDigest = alternateDigest(candidate.Basis.ManagedStateDigest)
			},
			basisState: func(result readiness.PreflightResult) string { return result.Basis.ManagedState.Status },
			blocker:    readiness.CodeManagedStateDrift,
		},
		{
			name: "observed-state",
			mutate: func(candidate *upmplan.Plan) {
				candidate.Basis.ObservedStateDigest = alternateDigest(candidate.Basis.ObservedStateDigest)
			},
			basisState: func(result readiness.PreflightResult) string { return result.Basis.ObservedState.Status },
			blocker:    readiness.CodeObservedStateDrift,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace, kubeconfig := managedPlanWorkspace(t)
			now := time.Date(2026, 7, 17, 6, 7, 8, 0, time.UTC)
			created, appErr := New(newPlanFixtureRunner(kubeconfig)).PlanVMStart(context.Background(), workspace, workspace, "k8s-3", now)
			if appErr != nil {
				t.Fatalf("PlanVMStart() error = %#v", appErr)
			}
			drifted := reidentifyPlan(t, created, test.mutate)
			if _, err := upmplan.NewStore(workspace).Save(drifted); err != nil {
				t.Fatalf("save drift fixture: %v", err)
			}
			before := snapshotTree(t, workspace)
			commandRunner := newPlanFixtureRunner(kubeconfig)
			result, appErr := New(commandRunner).PreflightPlan(context.Background(), workspace, workspace, drifted.PlanID, func() time.Time {
				return now.Add(5 * time.Minute)
			})
			if appErr != nil {
				t.Fatalf("PreflightPlan() error = %#v", appErr)
			}
			if result.PreflightStatus != readiness.PreflightBlocked || test.basisState(result) != readiness.BasisDrift || !containsString(result.Blockers, test.blocker) {
				t.Fatalf("preflight = %#v, want one %s basis drift", result, test.name)
			}
			matchCount := 0
			for _, status := range []string{result.Basis.Config.Status, result.Basis.ManagedState.Status, result.Basis.ObservedState.Status} {
				if status == readiness.BasisMatch {
					matchCount++
				}
			}
			if matchCount != 2 {
				t.Fatalf("basis = %#v, want exactly one DRIFT and two MATCH", result.Basis)
			}
			assertReadOnlyCommands(t, commandRunner.commands)
			if after := snapshotTree(t, workspace); !reflect.DeepEqual(after, before) {
				t.Fatalf("preflight drift check changed workspace\nbefore: %#v\nafter:  %#v", before, after)
			}
			assertPhase2b1StateAbsent(t, workspace)
		})
	}
}

func managedWorkspace(t *testing.T, unsafe bool) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	configContents, err := os.ReadFile(filepath.Join(repository, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	if unsafe {
		configContents = append(configContents, []byte("\nsystem(\"touch /tmp/unsafe\")\n")...)
	}
	workspace := t.TempDir()
	vagrantfile := []byte("# trusted fixture\n")
	if err := os.WriteFile(filepath.Join(workspace, "Vagrantfile"), vagrantfile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "vagrant"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "vagrant", "config.rb"), configContents, 0o600); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"apiVersion":    "upmctl.upm.io/v1alpha1",
		"kind":          "ManagedEnvironment",
		"environmentId": "env-test",
		"workspace":     workspace,
		"files": map[string]string{
			"Vagrantfile":       digest(vagrantfile),
			"vagrant/config.rb": digest(configContents),
		},
		"adoption": managedTestAdoption(),
	}
	stateContents, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".upmctl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".upmctl", "state.json"), stateContents, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func digest(contents []byte) string {
	value := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(value[:])
}

func managedPlanWorkspace(t *testing.T) (string, string) {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	configContents, err := os.ReadFile(filepath.Join(repository, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	configContents = []byte(strings.Replace(string(configContents), "$num_instances = 5", "$num_instances = 3", 1))
	workspace := t.TempDir()
	vagrantfile := []byte("# trusted plan fixture\n")
	kubeconfigContents := []byte("apiVersion: v1\nkind: Config\n")
	kubeconfig := filepath.Join(workspace, "inventory", "sample", "artifacts", "admin.conf")
	for path, contents := range map[string][]byte{
		filepath.Join(workspace, "Vagrantfile"):          vagrantfile,
		filepath.Join(workspace, "vagrant", "config.rb"): configContents,
		kubeconfig: kubeconfigContents,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	machines := map[string]string{
		"k8s-1": "11111111-1111-4111-8111-111111111111",
		"k8s-2": "22222222-2222-4222-8222-222222222222",
		"k8s-3": "33333333-3333-4333-8333-333333333333",
	}
	for name, uuid := range machines {
		path := filepath.Join(workspace, ".vagrant", "machines", name, "libvirt", "id")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(uuid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{
		"apiVersion": "upmctl.upm.io/v1alpha1", "kind": "ManagedEnvironment",
		"environmentId": "env-plan-test", "workspace": workspace,
		"files": map[string]string{
			"Vagrantfile": digest(vagrantfile), "vagrant/config.rb": digest(configContents),
			"inventory/sample/artifacts/admin.conf": digest(kubeconfigContents),
		},
		"machines": machines,
		"adoption": managedTestAdoption(),
	}
	stateContents, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".upmctl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".upmctl", "state.json"), stateContents, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace, kubeconfig
}

func managedTestAdoption() map[string]any {
	return map[string]any{
		"adoptedAt":     "2026-07-17T12:00:00Z",
		"actor":         map[string]any{"subject": "os-user:1000", "uid": "1000", "username": "operator", "hostname": "test-host", "source": "human-cli", "authMethod": "interactive-tty"},
		"humanPresence": map[string]any{"method": "typed-challenge", "terminal": "/dev/tty", "challengeDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "confirmedAt": "2026-07-17T12:00:00Z"},
		"reason":        "test fixture adoption", "requestId": "req-app-test", "cliVersion": "0.1.0-test",
	}
}

func newPlanFixtureRunner(kubeconfig string) *planFixtureRunner {
	results := map[string]runner.Result{
		"vagrant [status --machine-readable]":                         {Stdout: "1,k8s-1,state,running\n2,k8s-2,state,running\n3,k8s-3,state,poweroff\n"},
		"virsh [list --all --name]":                                   {Stdout: "fixture_k8s-1\nfixture_k8s-2\nfixture_k8s-3\n"},
		"kubectl [--kubeconfig " + kubeconfig + " get nodes -o json]": {Stdout: `{"items":[{"metadata":{"name":"k8s-1","uid":"node-uid-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"k8s-2","uid":"node-uid-2"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`},
	}
	uuids := map[int]string{1: "11111111-1111-4111-8111-111111111111", 2: "22222222-2222-4222-8222-222222222222", 3: "33333333-3333-4333-8333-333333333333"}
	for index := 1; index <= 3; index++ {
		name := fmt.Sprintf("k8s-%d", index)
		uuid := uuids[index]
		results["virsh [domuuid fixture_"+name+"]"] = runner.Result{Stdout: uuid + "\n"}
		state := "running"
		if index == 3 {
			state = "shut off"
		}
		results["virsh [domstate "+uuid+"]"] = runner.Result{Stdout: state + "\n"}
		results["virsh [dominfo "+uuid+"]"] = runner.Result{Stdout: "Name: fixture_" + name + "\nCPU(s): 4\nMax memory: 4194304 KiB\n"}
		results["virsh [domblklist "+uuid+" --details]"] = runner.Result{Stdout: "Type Device Target Source\n------------------------------------------------\nfile disk vda /var/lib/libvirt/images/" + name + ".img\n"}
		results["vagrant [ssh-config "+name+"]"] = runner.Result{Stdout: fmt.Sprintf("HostName 192.168.100.%d\nPort 22\n", 100+index)}
		if index != 3 {
			results["vagrant [ssh "+name+" -c true]"] = runner.Result{}
		}
	}
	return &planFixtureRunner{results: results}
}

func assertNoExecutionFields(t *testing.T, contents []byte) {
	t.Helper()
	for _, field := range []string{"command", "shell", "executable", "argv", "sudo"} {
		if strings.Contains(string(contents), `"`+field+`"`) {
			t.Fatalf("plan JSON contains forbidden execution field %q: %s", field, contents)
		}
	}
}

func assertReadOnlyCommands(t *testing.T, commands []runner.Command) {
	t.Helper()
	for _, command := range commands {
		allowed := false
		switch command.Executable {
		case "vagrant":
			allowed = len(command.Args) >= 1 && (command.Args[0] == "status" || command.Args[0] == "ssh-config" ||
				(len(command.Args) == 4 && command.Args[0] == "ssh" && command.Args[2] == "-c" && command.Args[3] == "true"))
		case "virsh":
			allowed = len(command.Args) >= 1 && (command.Args[0] == "list" || command.Args[0] == "domuuid" || command.Args[0] == "domstate" || command.Args[0] == "dominfo" || command.Args[0] == "domblklist")
		case "kubectl":
			allowed = len(command.Args) >= 3 && command.Args[2] == "get"
		}
		if !allowed {
			t.Fatalf("Plan generation executed a non-read-only command: %#v", command)
		}
	}
}

func reidentifyPlan(t *testing.T, candidate upmplan.Plan, mutate func(*upmplan.Plan)) upmplan.Plan {
	t.Helper()
	mutate(&candidate)
	digest, err := candidate.ExpectedDigest()
	if err != nil {
		t.Fatal(err)
	}
	candidate.PlanDigest = digest
	planID, err := candidate.ExpectedID()
	if err != nil {
		t.Fatal(err)
	}
	candidate.PlanID = planID
	if err := upmplan.Validate(candidate); err != nil {
		t.Fatalf("drift fixture is invalid: %v", err)
	}
	return candidate
}

func alternateDigest(current string) string {
	candidate := "sha256:" + strings.Repeat("a", 64)
	if candidate == current {
		return "sha256:" + strings.Repeat("b", 64)
	}
	return candidate
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var snapshot []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := fmt.Sprintf("%s|%s|%04o|%d", filepath.ToSlash(relative), info.Mode().Type(), info.Mode().Perm(), info.Size())
		if info.Mode().IsRegular() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			record += "|" + digest(contents)
		}
		snapshot = append(snapshot, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(snapshot)
	return snapshot
}

func assertPhase2b1StateAbsent(t *testing.T, workspace string) {
	t.Helper()
	for _, name := range []string{"operations", "approvals", "locks"} {
		path := filepath.Join(workspace, ".upmctl", name)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("Phase 2b1 unexpectedly created %s: %v", path, err)
		}
	}
}
