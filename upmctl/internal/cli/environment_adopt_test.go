package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/terminal"
)

func TestEnvironmentAdoptRequiresHumanTTYAndPublishesAuditedState(t *testing.T) {
	workspace := cliLegacyWorkspace(t)
	commandRunner := &cliCountingRunner{}
	var stdout, stderr, ttyOutput bytes.Buffer
	command := New(app.New(commandRunner), &stdout, &stderr)
	command.now = func() time.Time { return time.Date(2026, 7, 17, 13, 14, 15, 0, time.UTC) }
	command.challenge = func() (string, error) { return "CONFIRM-A1B2C3D4", nil }
	command.openTTY = func() (terminal.HumanTerminal, error) {
		return terminal.New(strings.NewReader("verified exact workspace identity\nCONFIRM-A1B2C3D4\n"), &ttyOutput), nil
	}
	exitCode := command.Run([]string{
		"environment", "adopt", "--environment-id", "env-cli-adopt",
		"--workspace", workspace, "--output", "json", "--request-id", "req-cli-adopt",
	})
	if exitCode != 0 {
		t.Fatalf("Run() exit=%d stderr=%s", exitCode, stderr.String())
	}
	if commandRunner.calls != 0 {
		t.Fatalf("runner calls = %d, adoption must not execute external commands", commandRunner.calls)
	}
	if strings.Contains(stdout.String(), "Adoption reason") || !strings.Contains(ttyOutput.String(), "Bound file: Vagrantfile sha256:") || !strings.Contains(ttyOutput.String(), "Machine: k8s-5") {
		t.Fatalf("TTY/stdout boundary violated\nstdout=%s\ntty=%s", stdout.String(), ttyOutput.String())
	}
	var envelope struct {
		Kind string `json:"kind"`
		Data struct {
			EnvironmentID string `json:"environmentId"`
			Adoption      struct {
				Reason    string `json:"reason"`
				RequestID string `json:"requestId"`
				Actor     struct {
					Source     string `json:"source"`
					AuthMethod string `json:"authMethod"`
				} `json:"actor"`
				HumanPresence struct {
					Method string `json:"method"`
				} `json:"humanPresence"`
			} `json:"adoption"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Kind != "ManagedEnvironment" || envelope.Data.EnvironmentID != "env-cli-adopt" ||
		envelope.Data.Adoption.Reason != "verified exact workspace identity" || envelope.Data.Adoption.RequestID != "req-cli-adopt" ||
		envelope.Data.Adoption.Actor.Source != "human-cli" || envelope.Data.Adoption.Actor.AuthMethod != "interactive-tty" ||
		envelope.Data.Adoption.HumanPresence.Method != "typed-challenge" {
		t.Fatalf("envelope = %#v", envelope)
	}
	assertCLIPathMode(t, filepath.Join(workspace, ".upmctl"), 0o700)
	assertCLIPathMode(t, filepath.Join(workspace, ".upmctl", "state.json"), 0o600)
}

func TestEnvironmentAdoptRejectsNonTTYWithoutWritingState(t *testing.T) {
	workspace := cliLegacyWorkspace(t)
	var stdout, stderr bytes.Buffer
	command := New(app.New(&cliCountingRunner{}), &stdout, &stderr)
	command.openTTY = func() (terminal.HumanTerminal, error) { return nil, errors.New("no controlling tty") }
	if exitCode := command.Run([]string{"environment", "adopt", "--environment-id", "env-no-tty", "--workspace", workspace, "--output", "json"}); exitCode != 3 {
		t.Fatalf("exit=%d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "UPMCTL_HUMAN_TTY_REQUIRED") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-TTY adoption created control state: %v", err)
	}
}

func TestEnvironmentAdoptContractParsingAndCanonicalLoggingName(t *testing.T) {
	for _, invalid := range [][]string{
		{"environment", "adopt", "--environment-id", "INVALID", "--workspace", "/tmp/example"},
		{"environment", "adopt", "--environment-id", "env-one", "--environment-id", "env-two", "--workspace", "/tmp/example"},
		{"environment", "adopt", "--environment-id", "env-one"},
	} {
		var stdout, stderr bytes.Buffer
		command := New(app.New(noOpRunner{}), &stdout, &stderr)
		if exit := command.Run(invalid); exit != 2 {
			t.Fatalf("Run(%v) exit=%d stderr=%s", invalid, exit, stderr.String())
		}
	}
	if got := canonicalCommand([]string{"environment", "adopt", "--environment-id", "secret"}); got != "environment adopt" {
		t.Fatalf("canonicalCommand() = %q", got)
	}
}

func cliLegacyWorkspace(t *testing.T) string {
	t.Helper()
	workspace := t.TempDir()
	cliWriteFixture(t, filepath.Join(workspace, "Vagrantfile"), "# legacy Vagrantfile fixture\n", 0o644)
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	config, err := os.ReadFile(filepath.Join(repository, "vagrant_setup_scripts", "vagrant-config", "nat_network-config.rb"))
	if err != nil {
		t.Fatal(err)
	}
	cliWriteFixture(t, filepath.Join(workspace, "vagrant", "config.rb"), string(config), 0o600)
	for index := 1; index <= 5; index++ {
		uuid := fmt.Sprintf("%08d-2222-4222-8222-%012d\n", index, index)
		cliWriteFixture(t, filepath.Join(workspace, ".vagrant", "machines", fmt.Sprintf("k8s-%d", index), "libvirt", "id"), uuid, 0o600)
	}
	return workspace
}

func cliWriteFixture(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func assertCLIPathMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode=%04o want=%04o", path, info.Mode().Perm(), want)
	}
}
