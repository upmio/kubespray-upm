package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
	"github.com/upmio/kubespray-upm/upmctl/internal/terminal"
)

type cliRunnerFunc func(context.Context, runner.Command) (runner.Result, error)

func (f cliRunnerFunc) Run(ctx context.Context, command runner.Command) (runner.Result, error) {
	return f(ctx, command)
}

func TestSuccessfulAdoptionRuntimeLogExcludesTrustBoundarySecrets(t *testing.T) {
	workspace := cliLegacyWorkspace(t)
	logPath := filepath.Join(t.TempDir(), "runtime.jsonl")
	reason := "customer incident CHG-4242 exact environment identity"
	challenge := "CONFIRM-DEADBEEF"
	var stdout, stderr, ttyOutput bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	command.challenge = func() (string, error) { return challenge, nil }
	command.openTTY = func() (terminal.HumanTerminal, error) {
		return terminal.New(strings.NewReader(reason+"\n"+challenge+"\n"), &ttyOutput), nil
	}
	exitCode := command.Run([]string{
		"environment", "adopt", "--environment-id", "env-sensitive-adoption",
		"--workspace", workspace, "--output", "json", "--request-id", "req-security-adopt",
		"--log-file", logPath,
	})
	if exitCode != 0 {
		t.Fatalf("Run() exit=%d stderr=%s tty=%s", exitCode, stderr.String(), ttyOutput.String())
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		workspace, "env-sensitive-adoption", reason, challenge,
		"00000001-2222-4222-8222-000000000001", "Vagrantfile", "state.json",
	} {
		if bytes.Contains(contents, []byte(forbidden)) {
			t.Fatalf("runtime log contains trust-boundary value %q: %s", forbidden, contents)
		}
	}
	if !bytes.Contains(contents, []byte(`"command":"environment adopt"`)) ||
		!bytes.Contains(contents, []byte(`"requestId":"req-security-adopt"`)) {
		t.Fatalf("runtime log lost allowed correlation fields: %s", contents)
	}
}

func TestUnknownCommandRuntimeLogDoesNotCopyUserInput(t *testing.T) {
	secret := "customer-secret-token-987654"
	logPath := filepath.Join(t.TempDir(), "runtime.jsonl")
	var stdout, stderr bytes.Buffer
	command := New(app.New(noOpRunner{}), &stdout, &stderr)
	if exitCode := command.Run([]string{secret, "--log-file", logPath, "--output", "json"}); exitCode != 3 {
		t.Fatalf("Run() exit=%d stderr=%s", exitCode, stderr.String())
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte(secret)) || !bytes.Contains(contents, []byte(`"command":"unknown"`)) {
		t.Fatalf("unsafe unknown-command log: %s", contents)
	}
}

func TestVMObservationTimeoutReturnsStableInterruptedError(t *testing.T) {
	workspace, _ := makeManagedPlanWorkspace(t)
	blockingRunner := cliRunnerFunc(func(ctx context.Context, _ runner.Command) (runner.Result, error) {
		<-ctx.Done()
		return runner.Result{ExitCode: -1}, ctx.Err()
	})
	var stdout, stderr bytes.Buffer
	command := New(app.New(blockingRunner), &stdout, &stderr)
	exitCode := command.Run([]string{"vm", "list", "--workspace", workspace, "--timeout", "1ms", "--output", "json"})
	if exitCode != 6 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_INTERRUPTED")) {
		t.Fatalf("Run() exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestMissingObservationDependenciesReturnExplicitUnavailableSources(t *testing.T) {
	workspace, _ := makeManagedPlanWorkspace(t)
	missingRunner := cliRunnerFunc(func(_ context.Context, command runner.Command) (runner.Result, error) {
		return runner.Result{ExitCode: -1}, errors.New(command.Executable + ": executable file not found")
	})
	var stdout, stderr bytes.Buffer
	command := New(app.New(missingRunner), &stdout, &stderr)
	exitCode := command.Run([]string{"vm", "list", "--workspace", workspace, "--output", "json"})
	if exitCode != 0 || stderr.Len() != 0 {
		t.Fatalf("Run() exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	for _, required := range []string{
		`"vagrant": "unavailable"`, `"libvirt": "unavailable"`, `"kubernetes": "unavailable"`,
		`"VAGRANT_STATUS_UNAVAILABLE"`, `"LIBVIRT_INVENTORY_UNAVAILABLE"`, `"KUBERNETES_API_UNAVAILABLE"`,
	} {
		if !bytes.Contains(stdout.Bytes(), []byte(required)) {
			t.Fatalf("missing dependency result does not disclose %q: %s", required, stdout.String())
		}
	}
}

func TestNonTTYRevocationStopsBeforeWorkspaceOrDependencyAccess(t *testing.T) {
	workspace := t.TempDir()
	counting := &cliCountingRunner{}
	var stdout, stderr bytes.Buffer
	command := New(app.New(counting), &stdout, &stderr)
	command.openTTY = func() (terminal.HumanTerminal, error) { return nil, errors.New("no controlling tty") }
	approvalID := "approval-" + strings.Repeat("a", 64)
	exitCode := command.Run([]string{"approval", "revoke", approvalID, "--workspace", workspace, "--output", "json"})
	if exitCode != 3 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_HUMAN_TTY_REQUIRED")) {
		t.Fatalf("Run() exit=%d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if counting.calls != 0 {
		t.Fatalf("non-TTY revoke invoked %d external commands", counting.calls)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-TTY revoke accessed control state: %v", err)
	}
}

func TestUnsupportedOperationsFailClosedWithoutRunnerCalls(t *testing.T) {
	tests := [][]string{
		{"vm", "stop", "k8s-3"},
		{"vm", "restart", "k8s-3"},
		{"plan", "vm", "stop", "--node", "k8s-3"},
		{"node", "add", "--node", "k8s-6"},
		{"node", "remove", "--node", "k8s-5"},
		{"addon", "install", "prometheus"},
		{"operation", "list"},
		{"apply", "--plan-id", "plan-" + strings.Repeat("b", 64)},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			counting := &cliCountingRunner{}
			var stdout, stderr bytes.Buffer
			command := New(app.New(counting), &stdout, &stderr)
			exitCode := command.Run(append(args, "--output", "json"))
			if exitCode != 3 || !bytes.Contains(stderr.Bytes(), []byte("UPMCTL_NOT_IMPLEMENTED")) {
				t.Fatalf("Run(%v) exit=%d stdout=%s stderr=%s", args, exitCode, stdout.String(), stderr.String())
			}
			if counting.calls != 0 {
				t.Fatalf("Run(%v) invoked %d external commands", args, counting.calls)
			}
		})
	}
}
