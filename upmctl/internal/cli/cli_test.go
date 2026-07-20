package cli

import (
	"bytes"
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

	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
	"github.com/upmio/kubespray-upm/upmctl/internal/terminal"
)

type noOpRunner struct{}

func (noOpRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	return runner.Result{}, nil
}

type cliCountingRunner struct {
	calls int
}

func (r *cliCountingRunner) Run(context.Context, runner.Command) (runner.Result, error) {
	r.calls++
	return runner.Result{}, nil
}

type cliPlanRunner struct {
	results    map[string]runner.Result
	commands   []runner.Command
	unexpected []runner.Command
}

func (r *cliPlanRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	r.commands = append(r.commands, command)
	key := command.Executable + " " + fmt.Sprint(command.Args)
	result, ok := r.results[key]
	if !ok {
		r.unexpected = append(r.unexpected, command)
		return runner.Result{}, fmt.Errorf("unexpected or non-read-only command: %s", key)
	}
	return result, nil
}

func TestVersionJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	command.now = func() time.Time { return time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC) }

	exitCode := command.Run([]string{"version", "--output", "json", "--request-id", "req-test"})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope["kind"] != "Version" || envelope["requestId"] != "req-test" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestRuntimeLogPreservesJSONStdoutAndRecordsLifecycle(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "upmctl.jsonl")
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	command.now = func() time.Time { return time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC) }

	exitCode := command.Run([]string{"version", "--output", "json", "--request-id", "req-logged", "--log-file", logPath})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one clean JSON document: %v; stdout=%q", err, stdout.String())
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2: %q", len(lines), contents)
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL event %q: %v", line, err)
		}
		if event["requestId"] != "req-logged" || event["command"] != "version" {
			t.Fatalf("event = %#v", event)
		}
	}
	if !strings.Contains(lines[0], `"event":"start"`) || !strings.Contains(lines[1], `"event":"complete"`) || !strings.Contains(lines[1], `"exitCode":0`) {
		t.Fatalf("unexpected lifecycle: %q", contents)
	}
}

func TestRuntimeLogErrorDoesNotRecordArgumentsOrApprovalSecrets(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	logPath := filepath.Join(t.TempDir(), "upmctl.jsonl")
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	planID := "plan-" + strings.Repeat("0", 64)

	exitCode := command.Run([]string{"approval", "grant", "--plan-id", planID, "--reason", "never-log-this-reason", "--output", "json", "--request-id", "req-secret", "--log-file", logPath})
	if exitCode != 2 {
		t.Fatalf("Run() exit code = %d, want 2; stderr=%s", exitCode, stderr.String())
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"never-log-this-reason", planID, "--reason", "Approval", "challenge"} {
		if bytes.Contains(contents, []byte(forbidden)) {
			t.Fatalf("runtime log contains sensitive/argument value %q: %s", forbidden, contents)
		}
	}
	if !bytes.Contains(contents, []byte(`"command":"approval grant"`)) || !bytes.Contains(contents, []byte(`"event":"error"`)) || !bytes.Contains(contents, []byte(`"errorCode":"UPMCTL_USAGE"`)) {
		t.Fatalf("runtime log = %s", contents)
	}
}

func TestRuntimeLogRejectsSymlinkWithoutRunningCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	directory := t.TempDir()
	target := filepath.Join(directory, "target.log")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "upmctl.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"version", "--output", "json", "--log-file", link})
	if exitCode != 70 {
		t.Fatalf("Run() exit code = %d, want 70", exitCode)
	}
	if stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_LOG_OPEN_FAILED")) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCanonicalCommandNeverCopiesUnknownAction(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "vm", "customer-secret"},
		{"approval", "customer-secret"},
		{"customer-secret"},
	} {
		if got := canonicalCommand(args); got != "unknown" {
			t.Fatalf("canonicalCommand(%q) = %q, want unknown", args, got)
		}
	}
	if got := canonicalCommand([]string{"plan", "vm", "start", "--node", "customer-secret"}); got != "plan vm start" {
		t.Fatalf("known command = %q", got)
	}
	if got := canonicalCommand([]string{"help", "approval"}); got != "help approval" {
		t.Fatalf("help command = %q", got)
	}
}

func TestCapabilitiesJSONWithoutEnvironment(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"capabilities", "--output", "json", "--request-id", "req-capabilities"})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope["kind"] != "Capabilities" {
		t.Fatalf("kind = %v, want Capabilities", envelope["kind"])
	}
}

func TestHelpCommandsAreStableOfflinePlainText(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "help", args: []string{"help"}, want: rootHelp},
		{name: "long flag", args: []string{"--help"}, want: rootHelp},
		{name: "short flag", args: []string{"-h"}, want: rootHelp},
		{name: "approval", args: []string{"help", "approval"}, want: approvalHelp},
		{name: "plan", args: []string{"help", "plan"}, want: planHelp},
		{name: "vm", args: []string{"help", "vm"}, want: vmHelp},
		{name: "environment", args: []string{"help", "environment"}, want: environmentHelp},
		{name: "output remains text", args: []string{"help", "plan", "--output", "json"}, want: planHelp},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			runner := &cliCountingRunner{}
			ttyCalls := 0
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command := New(app.New(runner), &stdout, &stderr)
			command.cwd = workspace
			command.openTTY = func() (terminal.HumanTerminal, error) {
				ttyCalls++
				return nil, errors.New("help must not open a TTY")
			}

			if code := command.Run(test.args); code != 0 {
				t.Fatalf("Run() exit code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.String() != test.want {
				t.Fatalf("stdout differs from stable help contract\nwant:\n%s\ngot:\n%s", test.want, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if runner.calls != 0 {
				t.Fatalf("help invoked environment runner %d times", runner.calls)
			}
			if ttyCalls != 0 {
				t.Fatalf("help opened the controlling TTY %d times", ttyCalls)
			}
			entries, err := os.ReadDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("help created workspace state: %v", entries)
			}
		})
	}
}

func TestHelpOnlyWritesExplicitRuntimeLog(t *testing.T) {
	workspace := t.TempDir()
	logDirectory := t.TempDir()
	logPath := filepath.Join(logDirectory, "runtime.jsonl")
	runner := &cliCountingRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(runner), &stdout, &stderr)
	command.cwd = workspace
	command.now = func() time.Time { return time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC) }

	if code := command.Run([]string{"help", "approval", "--log-file", logPath, "--request-id", "req-help"}); code != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != approvalHelp || stderr.Len() != 0 || runner.calls != 0 {
		t.Fatalf("stdout=%q stderr=%q runner calls=%d", stdout.String(), stderr.String(), runner.calls)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("help created workspace state: %v", entries)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(contents, []byte(`"command":"help approval"`)) || !bytes.Contains(contents, []byte(`"event":"complete"`)) {
		t.Fatalf("runtime log = %s", contents)
	}
}

func TestHelpRejectsUnknownTopicWithoutEnvironmentAccess(t *testing.T) {
	runner := &cliCountingRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(runner), &stdout, &stderr)

	if code := command.Run([]string{"help", "cluster"}); code != 2 {
		t.Fatalf("Run() exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_USAGE")) || runner.calls != 0 {
		t.Fatalf("stdout=%q stderr=%q runner calls=%d", stdout.String(), stderr.String(), runner.calls)
	}
}

func TestTimeoutMustBePositive(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"version", "--timeout=-1s", "--output=json"})
	if exitCode != 2 {
		t.Fatalf("Run() exit code = %d, want 2", exitCode)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_USAGE")) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMutationCommandReportsNotImplemented(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"plan", "vm", "stop", "--node", "k8s-3", "--output", "json"})
	if exitCode != 3 {
		t.Fatalf("Run() exit code = %d, want 3", exitCode)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_NOT_IMPLEMENTED")) || !bytes.Contains(stderr.Bytes(), []byte("Phase 2b2a")) || bytes.Contains(stderr.Bytes(), []byte("Phase 2a")) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestApplyReportsCurrentPhaseNotImplemented(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	planID := "plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	exitCode := command.Run([]string{"apply", "--plan-id", planID, "--output", "json"})
	if exitCode != 3 {
		t.Fatalf("Run() exit code = %d, want 3", exitCode)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_NOT_IMPLEMENTED")) || !bytes.Contains(stderr.Bytes(), []byte("Phase 2b2a")) || bytes.Contains(stderr.Bytes(), []byte("Phase 2b1")) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestPlanVMStartRequiresManagedWorkspace(t *testing.T) {
	workspace := makeLegacyWorkspace(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace})
	if exitCode != 3 {
		t.Fatalf("Run() exit code = %d, want 3", exitCode)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_WORKSPACE_UNTRUSTED")) {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(workspace, ".upmctl", "plans")); !os.IsNotExist(err) {
		t.Fatalf("legacy workspace unexpectedly gained a plan store: %v", err)
	}
}

func TestPlanVMStartJSONCreatesSinglePrivateReadOnlyPlan(t *testing.T) {
	workspace, kubeconfig := makeManagedPlanWorkspace(t)
	commandRunner := newCLIPlanRunner(kubeconfig)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(commandRunner), &stdout, &stderr)
	now := time.Date(2026, 7, 17, 4, 5, 6, 0, time.UTC)
	command.now = func() time.Time { return now }

	exitCode := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace, "--output", "json", "--request-id", "req-plan"})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope struct {
		Kind      string `json:"kind"`
		RequestID string `json:"requestId"`
		Data      struct {
			PlanID      string `json:"planId"`
			Disposition string `json:"disposition"`
			RiskLevel   string `json:"riskLevel"`
			CreatedAt   string `json:"createdAt"`
			ExpiresAt   string `json:"expiresAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if envelope.Kind != "Plan" || envelope.RequestID != "req-plan" || envelope.Data.Disposition != "ACTION_REQUIRED" || envelope.Data.RiskLevel != "R1" {
		t.Fatalf("unexpected Plan envelope: %#v", envelope)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, envelope.Data.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, envelope.Data.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.Sub(createdAt) != 30*time.Minute {
		t.Fatalf("plan TTL = %s, want 30m", expiresAt.Sub(createdAt))
	}

	plansDirectory := filepath.Join(workspace, ".upmctl", "plans")
	entries, err := os.ReadDir(plansDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != envelope.Data.PlanID+".json" {
		t.Fatalf("plan files = %#v, want only %s.json", entries, envelope.Data.PlanID)
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
	assertNoCLIExecutionFields(t, contents)
	if len(commandRunner.unexpected) != 0 {
		t.Fatalf("runner received unexpected commands: %#v", commandRunner.unexpected)
	}
	assertCLIReadOnlyCommands(t, commandRunner.commands)
}

func TestPlanVMStartRejectsInvalidNodeUsage(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"plan", "vm", "start", "--node", "k8s-9", "--output", "json"})
	if exitCode != 2 {
		t.Fatalf("Run() exit code = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_USAGE")) || !bytes.Contains(stderr.Bytes(), []byte("k8s-1 through k8s-8")) {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
}

func TestPhase2b1PlanAuditPreflightAndApplyBoundary(t *testing.T) {
	workspace, kubeconfig := makeManagedPlanWorkspace(t)
	now := time.Date(2026, 7, 17, 7, 8, 9, 0, time.UTC)
	generationRunner := newCLIPlanRunner(kubeconfig)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(generationRunner), &stdout, &stderr)
	command.now = func() time.Time { return now }

	exitCode := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace, "--output", "json"})
	if exitCode != 0 {
		t.Fatalf("plan generation exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var generated struct {
		Data struct {
			PlanID      string `json:"planId"`
			Disposition string `json:"disposition"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &generated); err != nil {
		t.Fatalf("plan output is not JSON: %v", err)
	}
	if generated.Data.Disposition != "ACTION_REQUIRED" || generated.Data.PlanID == "" {
		t.Fatalf("generated Plan = %#v", generated)
	}

	beforeAudit := snapshotCLITree(t, workspace)
	auditRunner := &cliCountingRunner{}
	stdout.Reset()
	stderr.Reset()
	command = New(app.New(auditRunner), &stdout, &stderr)
	command.now = func() time.Time { return now.Add(5 * time.Minute) }
	exitCode = command.Run([]string{"plan", "get", generated.Data.PlanID, "--workspace", workspace, "--output", "json"})
	if exitCode != 0 {
		t.Fatalf("plan get exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var inspection struct {
		Kind string `json:"kind"`
		Data struct {
			Expired            bool `json:"expired"`
			ExecutionAvailable bool `json:"executionAvailable"`
			Plan               struct {
				PlanID string `json:"planId"`
			} `json:"plan"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &inspection); err != nil {
		t.Fatalf("plan get output is not JSON: %v", err)
	}
	if inspection.Kind != "PlanInspection" || inspection.Data.Plan.PlanID != generated.Data.PlanID || inspection.Data.Expired || inspection.Data.ExecutionAvailable {
		t.Fatalf("inspection = %#v", inspection)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = command.Run([]string{"plan", "validate", generated.Data.PlanID, "--workspace", workspace, "--output", "json"})
	if exitCode != 0 {
		t.Fatalf("plan validate exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var validation struct {
		Kind string `json:"kind"`
		Data struct {
			ArtifactStatus       string   `json:"artifactStatus"`
			FreshnessStatus      string   `json:"freshnessStatus"`
			ConfigBinding        string   `json:"configBinding"`
			ManagedStateBinding  string   `json:"managedStateBinding"`
			ObservedStateBinding string   `json:"observedStateBinding"`
			ExecutionAvailable   bool     `json:"executionAvailable"`
			Blockers             []string `json:"blockers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &validation); err != nil {
		t.Fatalf("plan validate output is not JSON: %v", err)
	}
	if validation.Kind != "PlanValidation" || validation.Data.ArtifactStatus != "VALID" || validation.Data.FreshnessStatus != "CURRENT" ||
		validation.Data.ConfigBinding != "MATCH" || validation.Data.ManagedStateBinding != "MATCH" || validation.Data.ObservedStateBinding != "NOT_CHECKED" ||
		validation.Data.ExecutionAvailable || len(validation.Data.Blockers) != 0 {
		t.Fatalf("validation = %#v", validation)
	}
	if auditRunner.calls != 0 {
		t.Fatalf("plan get/validate runner calls = %d, want 0", auditRunner.calls)
	}
	if afterAudit := snapshotCLITree(t, workspace); !reflect.DeepEqual(afterAudit, beforeAudit) {
		t.Fatalf("plan get/validate changed workspace\nbefore: %#v\nafter:  %#v", beforeAudit, afterAudit)
	}

	preflightRunner := newCLIPlanRunner(kubeconfig)
	stdout.Reset()
	stderr.Reset()
	command = New(app.New(preflightRunner), &stdout, &stderr)
	command.now = func() time.Time { return now.Add(10 * time.Minute) }
	exitCode = command.Run([]string{"preflight", "--plan-id", generated.Data.PlanID, "--workspace", workspace, "--output", "json"})
	if exitCode != 3 {
		t.Fatalf("preflight exit code = %d, want 3; stderr = %s", exitCode, stderr.String())
	}
	var preflight struct {
		Kind string `json:"kind"`
		Data struct {
			PreflightStatus    string `json:"preflightStatus"`
			ApplyDecision      string `json:"applyDecision"`
			ExecutionAvailable bool   `json:"executionAvailable"`
			ApprovalStatus     string `json:"approvalStatus"`
			Basis              struct {
				Config struct {
					Status string `json:"status"`
				} `json:"config"`
				ManagedState struct {
					Status string `json:"status"`
				} `json:"managedState"`
				ObservedState struct {
					Status string `json:"status"`
				} `json:"observedState"`
			} `json:"basis"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &preflight); err != nil {
		t.Fatalf("preflight output is not JSON: %v\n%s", err, stdout.String())
	}
	if preflight.Kind != "PreflightResult" || preflight.Data.PreflightStatus != "PASSED" || preflight.Data.ApplyDecision != "BLOCKED" ||
		preflight.Data.ExecutionAvailable || preflight.Data.ApprovalStatus != "MISSING" ||
		preflight.Data.Basis.Config.Status != "MATCH" || preflight.Data.Basis.ManagedState.Status != "MATCH" || preflight.Data.Basis.ObservedState.Status != "MATCH" {
		t.Fatalf("preflight = %#v", preflight)
	}
	if len(preflightRunner.commands) == 0 || len(preflightRunner.unexpected) != 0 {
		t.Fatalf("preflight commands/unexpected = %#v/%#v", preflightRunner.commands, preflightRunner.unexpected)
	}
	assertCLIReadOnlyCommands(t, preflightRunner.commands)
	if afterPreflight := snapshotCLITree(t, workspace); !reflect.DeepEqual(afterPreflight, beforeAudit) {
		t.Fatalf("preflight changed workspace\nbefore: %#v\nafter:  %#v", beforeAudit, afterPreflight)
	}

	applyRunner := &cliCountingRunner{}
	stdout.Reset()
	stderr.Reset()
	command = New(app.New(applyRunner), &stdout, &stderr)
	exitCode = command.Run([]string{"apply", "--plan-id", generated.Data.PlanID, "--workspace", workspace, "--output", "json"})
	if exitCode != 3 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_NOT_IMPLEMENTED")) {
		t.Fatalf("apply exit code/stdout/stderr = %d/%q/%q", exitCode, stdout.String(), stderr.String())
	}
	if applyRunner.calls != 0 {
		t.Fatalf("apply runner calls = %d, want 0", applyRunner.calls)
	}
	if afterApply := snapshotCLITree(t, workspace); !reflect.DeepEqual(afterApply, beforeAudit) {
		t.Fatalf("apply changed workspace\nbefore: %#v\nafter:  %#v", beforeAudit, afterApply)
	}
	assertCLIPhase2b1StateAbsent(t, workspace)
}

func TestPhase2b1PlanIdentifiersAndMissingPlanErrors(t *testing.T) {
	workspace, kubeconfig := makeManagedPlanWorkspace(t)
	now := time.Date(2026, 7, 17, 8, 9, 10, 0, time.UTC)
	generationRunner := newCLIPlanRunner(kubeconfig)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(generationRunner), &stdout, &stderr)
	command.now = func() time.Time { return now }
	if exitCode := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace, "--output", "json"}); exitCode != 0 {
		t.Fatalf("plan generation exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	before := snapshotCLITree(t, workspace)

	for _, args := range [][]string{
		{"plan", "get", "../bad", "--workspace", workspace, "--output", "json"},
		{"plan", "validate", "plan-NOT-HEX", "--workspace", workspace, "--output", "json"},
		{"preflight", "--plan-id", "bad", "--workspace", workspace, "--output", "json"},
		{"apply", "--plan-id", "bad", "--workspace", workspace, "--output", "json"},
	} {
		runner := &cliCountingRunner{}
		stdout.Reset()
		stderr.Reset()
		command = New(app.New(runner), &stdout, &stderr)
		if exitCode := command.Run(args); exitCode != 2 {
			t.Fatalf("Run(%q) exit code = %d, want 2; stderr = %s", args, exitCode, stderr.String())
		}
		if !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_USAGE")) || runner.calls != 0 {
			t.Fatalf("Run(%q) runner/stderr = %d/%q", args, runner.calls, stderr.String())
		}
	}

	missingID := "plan-" + strings.Repeat("0", 64)
	missingRunner := &cliCountingRunner{}
	stdout.Reset()
	stderr.Reset()
	command = New(app.New(missingRunner), &stdout, &stderr)
	exitCode := command.Run([]string{"plan", "get", missingID, "--workspace", workspace, "--output", "json", "--request-id", "req-missing"})
	if exitCode != 3 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_PLAN_NOT_FOUND")) || !bytes.Contains(stderr.Bytes(), []byte("req-missing")) {
		t.Fatalf("missing Plan exit/stdout/stderr = %d/%q/%q", exitCode, stdout.String(), stderr.String())
	}
	if missingRunner.calls != 0 {
		t.Fatalf("missing Plan runner calls = %d, want 0", missingRunner.calls)
	}
	if after := snapshotCLITree(t, workspace); !reflect.DeepEqual(after, before) {
		t.Fatalf("invalid/missing Plan requests changed workspace\nbefore: %#v\nafter:  %#v", before, after)
	}
	assertCLIPhase2b1StateAbsent(t, workspace)
}

func TestPhase2b2aApprovalGrantInspectRevokeAndPreflightStates(t *testing.T) {
	workspace, kubeconfig := makeManagedPlanWorkspace(t)
	base := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	var stdout, stderr bytes.Buffer

	generationRunner := newCLIPlanRunner(kubeconfig)
	command := New(app.New(generationRunner), &stdout, &stderr)
	command.now = func() time.Time { return base }
	if code := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace, "--output", "json", "--request-id", "req-plan"}); code != 0 {
		t.Fatalf("plan exit = %d, stderr = %s", code, stderr.String())
	}
	var planEnvelope struct {
		Data struct {
			PlanID string `json:"planId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &planEnvelope); err != nil {
		t.Fatal(err)
	}
	planID := planEnvelope.Data.PlanID

	stdout.Reset()
	stderr.Reset()
	grantRunner := newCLIPlanRunner(kubeconfig)
	command = New(app.New(grantRunner), &stdout, &stderr)
	command.now = func() time.Time { return base.Add(time.Minute) }
	var grantTTY bytes.Buffer
	command.openTTY = func() (terminal.HumanTerminal, error) {
		return terminal.New(strings.NewReader("planned maintenance\nCONFIRM-0123ABCD\n"), &grantTTY), nil
	}
	command.challenge = func() (string, error) { return "CONFIRM-0123ABCD", nil }
	if code := command.Run([]string{"approval", "grant", "--plan-id", planID, "--workspace", workspace, "--output", "json", "--request-id", "req-grant"}); code != 0 {
		t.Fatalf("grant exit = %d, stderr = %s, tty = %s", code, stderr.String(), grantTTY.String())
	}
	var approvalEnvelope struct {
		Kind string `json:"kind"`
		Data struct {
			ApprovalID    string `json:"approvalId"`
			PlanID        string `json:"planId"`
			Reason        string `json:"reason"`
			HumanPresence struct {
				Terminal string `json:"terminal"`
			} `json:"humanPresence"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &approvalEnvelope); err != nil {
		t.Fatalf("grant output JSON: %v\n%s", err, stdout.String())
	}
	if approvalEnvelope.Kind != "Approval" || approvalEnvelope.Data.PlanID != planID || approvalEnvelope.Data.Reason != "planned maintenance" || approvalEnvelope.Data.HumanPresence.Terminal != "/dev/tty" {
		t.Fatalf("approval envelope = %#v", approvalEnvelope)
	}
	approvalID := approvalEnvelope.Data.ApprovalID
	if approvalID == "" {
		t.Fatal("approval ID is empty")
	}
	if !strings.Contains(grantTTY.String(), planID) || !strings.Contains(grantTTY.String(), "Apply remains BLOCKED") {
		t.Fatalf("grant TTY did not show immutable scope: %q", grantTTY.String())
	}
	approvalPath := filepath.Join(workspace, ".upmctl", "approvals", "by-plan", planID+".json")
	assertPrivateRegularFile(t, approvalPath)
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl", "admissions", planID+".json")); !os.IsNotExist(err) {
		t.Fatalf("grant unexpectedly created admission: %v", err)
	}
	assertCLIReadOnlyCommands(t, grantRunner.commands)

	stdout.Reset()
	stderr.Reset()
	queryRunner := &cliCountingRunner{}
	command = New(app.New(queryRunner), &stdout, &stderr)
	command.now = func() time.Time { return base.Add(2 * time.Minute) }
	if code := command.Run([]string{"approval", "get", approvalID, "--workspace", workspace, "--output", "json"}); code != 0 {
		t.Fatalf("get exit = %d, stderr = %s", code, stderr.String())
	}
	if queryRunner.calls != 0 || !bytes.Contains(stdout.Bytes(), []byte(`"status": "APPROVED"`)) {
		t.Fatalf("get runner/stdout = %d/%s", queryRunner.calls, stdout.String())
	}
	stdout.Reset()
	if code := command.Run([]string{"approval", "list", "--plan-id", planID, "--workspace", workspace, "--output", "json"}); code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(approvalID)) || queryRunner.calls != 0 {
		t.Fatalf("list runner/stdout = %d/%s", queryRunner.calls, stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	approvedRunner := newCLIPlanRunner(kubeconfig)
	command = New(app.New(approvedRunner), &stdout, &stderr)
	command.now = func() time.Time { return base.Add(2 * time.Minute) }
	if code := command.Run([]string{"preflight", "--plan-id", planID, "--workspace", workspace, "--output", "json"}); code != 3 {
		t.Fatalf("approved preflight exit = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"approvalStatus": "APPROVED"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"applyDecision": "BLOCKED"`)) {
		t.Fatalf("approved preflight = %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	command = New(app.New(&cliCountingRunner{}), &stdout, &stderr)
	command.now = func() time.Time { return base.Add(3 * time.Minute) }
	var revokeTTY bytes.Buffer
	command.openTTY = func() (terminal.HumanTerminal, error) {
		return terminal.New(strings.NewReader("maintenance cancelled\nCONFIRM-FEEDBEEF\n"), &revokeTTY), nil
	}
	command.challenge = func() (string, error) { return "CONFIRM-FEEDBEEF", nil }
	if code := command.Run([]string{"approval", "revoke", approvalID, "--workspace", workspace, "--output", "json", "--request-id", "req-revoke"}); code != 0 {
		t.Fatalf("revoke exit = %d, stderr = %s, tty = %s", code, stderr.String(), revokeTTY.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"kind": "ApprovalRevocation"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"reason": "maintenance cancelled"`)) {
		t.Fatalf("revocation output = %s", stdout.String())
	}
	assertPrivateRegularFile(t, filepath.Join(workspace, ".upmctl", "admissions", planID+".json"))

	stdout.Reset()
	stderr.Reset()
	revokedRunner := newCLIPlanRunner(kubeconfig)
	command = New(app.New(revokedRunner), &stdout, &stderr)
	command.now = func() time.Time { return base.Add(4 * time.Minute) }
	if code := command.Run([]string{"preflight", "--plan-id", planID, "--workspace", workspace, "--output", "json"}); code != 3 {
		t.Fatalf("revoked preflight exit = %d, stderr = %s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte(`"approvalStatus": "REVOKED"`)) || !bytes.Contains(stdout.Bytes(), []byte(`"executionAvailable": false`)) {
		t.Fatalf("revoked preflight = %s", stdout.String())
	}
}

func TestApprovalWritesRequireControllingTTYBeforeObservation(t *testing.T) {
	workspace, kubeconfig := makeManagedPlanWorkspace(t)
	base := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	var stdout, stderr bytes.Buffer
	generationRunner := newCLIPlanRunner(kubeconfig)
	command := New(app.New(generationRunner), &stdout, &stderr)
	command.now = func() time.Time { return base }
	if code := command.Run([]string{"plan", "vm", "start", "--node", "k8s-3", "--workspace", workspace, "--output", "json"}); code != 0 {
		t.Fatalf("plan exit = %d", code)
	}
	var generated struct {
		Data struct {
			PlanID string `json:"planId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &generated); err != nil {
		t.Fatal(err)
	}
	before := snapshotCLITree(t, workspace)
	blockedRunner := &cliCountingRunner{}
	stdout.Reset()
	stderr.Reset()
	command = New(app.New(blockedRunner), &stdout, &stderr)
	command.openTTY = func() (terminal.HumanTerminal, error) { return nil, errors.New("no controlling tty") }
	code := command.Run([]string{"approval", "grant", "--plan-id", generated.Data.PlanID, "--workspace", workspace, "--output", "json"})
	if code != 3 || blockedRunner.calls != 0 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_HUMAN_TTY_REQUIRED")) {
		t.Fatalf("grant boundary = code %d calls %d stderr %s", code, blockedRunner.calls, stderr.String())
	}
	if after := snapshotCLITree(t, workspace); !reflect.DeepEqual(before, after) {
		t.Fatalf("non-TTY grant changed workspace\nbefore: %#v\nafter: %#v", before, after)
	}
	for _, forbidden := range [][]string{
		{"approval", "grant", "--plan-id", generated.Data.PlanID, "--reason", "bypass", "--workspace", workspace},
		{"approval", "grant", "--plan-id", generated.Data.PlanID, "--yes", "--workspace", workspace},
		{"approval", "revoke", "approval-" + strings.Repeat("0", 64), "--force", "--workspace", workspace},
	} {
		stdout.Reset()
		stderr.Reset()
		command = New(app.New(blockedRunner), &stdout, &stderr)
		if got := command.Run(forbidden); got != 2 {
			t.Fatalf("forbidden args %q exit = %d, stderr = %s", forbidden, got, stderr.String())
		}
	}
}

func TestConfigValidateLegacyWorkspace(t *testing.T) {
	workspace := makeLegacyWorkspace(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"config", "validate", "--workspace", workspace, "--output", "json"})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope struct {
		Kind string `json:"kind"`
		Data struct {
			Executable bool `json:"executable"`
			Validation struct {
				Status string `json:"status"`
				Valid  bool   `json:"valid"`
			} `json:"validation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope.Kind != "ConfigValidation" || !envelope.Data.Validation.Valid || envelope.Data.Executable {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestStatusLegacyWorkspaceDoesNotRequireExternalObservation(t *testing.T) {
	workspace := makeLegacyWorkspace(t)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)

	exitCode := command.Run([]string{"status", "--workspace", workspace, "--output", "json"})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, stderr = %s", exitCode, stderr.String())
	}
	var envelope struct {
		Data struct {
			Mode   string `json:"mode"`
			Health string `json:"health"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if envelope.Data.Mode != "legacy-read-only" || envelope.Data.Health != "UNKNOWN" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func makeLegacyWorkspace(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	contents, err := os.ReadFile(filepath.Join(repository, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "Vagrantfile"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "vagrant"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "vagrant", "config.rb"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func makeManagedPlanWorkspace(t *testing.T) (string, string) {
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
	vagrantfile := []byte("# trusted CLI plan fixture\n")
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
		"environmentId": "env-cli-plan-test", "workspace": workspace,
		"files": map[string]string{
			"Vagrantfile": cliDigest(vagrantfile), "vagrant/config.rb": cliDigest(configContents),
			"inventory/sample/artifacts/admin.conf": cliDigest(kubeconfigContents),
		},
		"machines": machines,
		"adoption": cliManagedTestAdoption(),
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

func cliManagedTestAdoption() map[string]any {
	return map[string]any{
		"adoptedAt":     "2026-07-17T12:00:00Z",
		"actor":         map[string]any{"subject": "os-user:1000", "uid": "1000", "username": "operator", "hostname": "test-host", "source": "human-cli", "authMethod": "interactive-tty"},
		"humanPresence": map[string]any{"method": "typed-challenge", "terminal": "/dev/tty", "challengeDigest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "confirmedAt": "2026-07-17T12:00:00Z"},
		"reason":        "test fixture adoption", "requestId": "req-cli-test", "cliVersion": "0.1.0-test",
	}
}

func cliDigest(contents []byte) string {
	value := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(value[:])
}

func newCLIPlanRunner(kubeconfig string) *cliPlanRunner {
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
	return &cliPlanRunner{results: results}
}

func assertNoCLIExecutionFields(t *testing.T, contents []byte) {
	t.Helper()
	for _, field := range []string{"command", "shell", "executable", "argv", "sudo"} {
		if strings.Contains(string(contents), `"`+field+`"`) {
			t.Fatalf("plan JSON contains forbidden execution field %q: %s", field, contents)
		}
	}
}

func assertCLIReadOnlyCommands(t *testing.T, commands []runner.Command) {
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
			t.Fatalf("Plan CLI executed a non-read-only command: %#v", command)
		}
	}
}

func snapshotCLITree(t *testing.T, root string) []string {
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
			record += "|" + cliDigest(contents)
		}
		snapshot = append(snapshot, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(snapshot)
	return snapshot
}

func assertCLIPhase2b1StateAbsent(t *testing.T, workspace string) {
	t.Helper()
	for _, name := range []string{"operations", "approvals", "locks"} {
		path := filepath.Join(workspace, ".upmctl", name)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("Phase 2b1 unexpectedly created %s: %v", path, err)
		}
	}
}

func assertPrivateRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %v/%04o, want regular/0600", path, info.Mode().Type(), info.Mode().Perm())
	}
}
