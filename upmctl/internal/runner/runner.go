package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type Command struct {
	Executable string
	Args       []string
	Dir        string
	Env        []string
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Runner interface {
	Run(ctx context.Context, command Command) (Result, error)
}

type ExecRunner struct{}

func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

func (r *ExecRunner) Run(ctx context.Context, command Command) (Result, error) {
	if command.Executable == "" {
		return Result{}, errors.New("runner: executable is required")
	}

	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Dir
	if command.Env != nil {
		cmd.Env = append(os.Environ(), command.Env...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err == nil {
		return result, nil
	}
	if contextErr := ctx.Err(); contextErr != nil {
		result.ExitCode = -1
		return result, fmt.Errorf("runner: execute %s: %w", command.Executable, contextErr)
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
		return result, fmt.Errorf("runner: %s exited with code %d: %w", command.Executable, result.ExitCode, err)
	}

	result.ExitCode = -1
	return result, fmt.Errorf("runner: execute %s: %w", command.Executable, err)
}
