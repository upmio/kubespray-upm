package runner

import (
	"context"
	"runtime"
	"testing"
)

func TestExecRunnerPreservesArgumentsWithoutShellExpansion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("printf fixture requires a Unix userspace")
	}
	result, err := NewExecRunner().Run(context.Background(), Command{
		Executable: "printf",
		Args:       []string{"%s", "$HOME;echo unsafe"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Stdout != "$HOME;echo unsafe" {
		t.Fatalf("Stdout = %q, want literal argument", result.Stdout)
	}
}
