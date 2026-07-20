package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/upmio/kubespray-upm/upmctl/internal/app"
)

func TestNoArgumentCommandsRejectTrailingArgumentsAndUnknownOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "version positional", args: []string{"version", "unexpected"}},
		{name: "version option", args: []string{"version", "--typo"}},
		{name: "capabilities positional", args: []string{"capabilities", "unexpected"}},
		{name: "capabilities option", args: []string{"capabilities", "--typo"}},
		{name: "status positional", args: []string{"status", "unexpected"}},
		{name: "status option", args: []string{"status", "--typo"}},
		{name: "context discover positional", args: []string{"context", "discover", "unexpected"}},
		{name: "context discover option", args: []string{"context", "discover", "--typo"}},
		{name: "config validate positional", args: []string{"config", "validate", "unexpected"}},
		{name: "config validate option", args: []string{"config", "validate", "--typo"}},
		{name: "unknown leading option", args: []string{"--typo", "capabilities"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			command := New(app.New(noOpRunner{}), &stdout, &stderr)
			exitCode := command.Run(append(test.args, "--output", "json"))
			if exitCode != 2 {
				t.Fatalf("Run(%v) exit=%d stdout=%s stderr=%s", test.args, exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), `"kind": "Error"`) ||
				!strings.Contains(stderr.String(), `"code": "UPMCTL_USAGE"`) {
				t.Fatalf("Run(%v) did not return a JSON usage error: stdout=%s stderr=%s", test.args, stdout.String(), stderr.String())
			}
		})
	}
}

func TestGlobalSingleValueOptionsRejectDuplicatesAndEmptyValues(t *testing.T) {
	logDirectory := t.TempDir()
	tests := []struct {
		name string
		args []string
	}{
		{name: "duplicate output", args: []string{"version", "--output", "json", "--output=json"}},
		{name: "duplicate workspace", args: []string{"--output", "json", "context", "discover", "--workspace", "/tmp/a", "--workspace=/tmp/b"}},
		{name: "duplicate request id", args: []string{"--output", "json", "version", "--request-id", "one", "--request-id=two"}},
		{name: "duplicate timeout", args: []string{"--output", "json", "version", "--timeout", "1s", "--timeout=2s"}},
		{name: "duplicate log file", args: []string{"--output", "json", "version", "--log-file", filepath.Join(logDirectory, "one.jsonl"), "--log-file=" + filepath.Join(logDirectory, "two.jsonl")}},
		{name: "duplicate no color", args: []string{"--output", "json", "version", "--no-color", "--no-color"}},
		{name: "empty workspace", args: []string{"context", "discover", "--workspace=", "--output", "json"}},
		{name: "empty request id", args: []string{"version", "--request-id=", "--output", "json"}},
		{name: "empty log file", args: []string{"version", "--log-file", "", "--output", "json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			command := New(app.New(noOpRunner{}), &stdout, &stderr)
			if exitCode := command.Run(test.args); exitCode != 2 {
				t.Fatalf("Run(%v) exit=%d stdout=%s stderr=%s", test.args, exitCode, stdout.String(), stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), `"code": "UPMCTL_USAGE"`) {
				t.Fatalf("Run(%v) did not return usage error: stdout=%s stderr=%s", test.args, stdout.String(), stderr.String())
			}
		})
	}
}
