package runner

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerPropagatesDeadlineExceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sleep fixture requires a Unix userspace")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	result, err := NewExecRunner().Run(ctx, Command{Executable: "sleep", Args: []string{"10"}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context.DeadlineExceeded", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("Run() exit code = %d, want -1 for an interrupted process", result.ExitCode)
	}
}

func TestExecRunnerMissingDependencyFailsWithoutShellFallback(t *testing.T) {
	name := "upmctl-definitely-missing-security-fixture"
	result, err := NewExecRunner().Run(context.Background(), Command{
		Executable: name,
		Args:       []string{"$(touch /tmp/upmctl-must-not-exist)", ";", "true"},
	})
	if err == nil || !strings.Contains(err.Error(), name) {
		t.Fatalf("Run() error = %v, want missing executable error", err)
	}
	if result.ExitCode != -1 {
		t.Fatalf("Run() exit code = %d, want -1", result.ExitCode)
	}
}
