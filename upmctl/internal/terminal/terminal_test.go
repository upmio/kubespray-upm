package terminal

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestReadReasonUsesInjectedTerminalWriter(t *testing.T) {
	var ttyOutput bytes.Buffer
	terminal := New(strings.NewReader("  planned maintenance  \n"), &ttyOutput)

	reason, err := terminal.ReadReason("Reason: ")
	if err != nil {
		t.Fatalf("ReadReason() error = %v", err)
	}
	if reason != "planned maintenance" {
		t.Fatalf("ReadReason() = %q", reason)
	}
	if ttyOutput.String() != "Reason: " {
		t.Fatalf("terminal output = %q", ttyOutput.String())
	}
}

func TestReadReasonRejectsBlankInput(t *testing.T) {
	terminal := New(strings.NewReader(" \t \n"), io.Discard)

	_, err := terminal.ReadReason("Reason: ")
	if !errors.Is(err, ErrEmptyReason) {
		t.Fatalf("ReadReason() error = %v, want ErrEmptyReason", err)
	}
}

func TestConfirmChallengeRequiresExactInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "exact", input: "CONFIRM-0123ABCD\n", want: true},
		{name: "different case", input: "confirm-0123abcd\n", want: false},
		{name: "leading whitespace", input: " CONFIRM-0123ABCD\n", want: false},
		{name: "trailing whitespace", input: "CONFIRM-0123ABCD \n", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var ttyOutput bytes.Buffer
			terminal := New(strings.NewReader(test.input), &ttyOutput)
			confirmed, err := terminal.ConfirmChallenge("Approve plan?", "CONFIRM-0123ABCD")
			if err != nil {
				t.Fatalf("ConfirmChallenge() error = %v", err)
			}
			if confirmed != test.want {
				t.Fatalf("ConfirmChallenge() = %v, want %v", confirmed, test.want)
			}
			if ttyOutput.String() != "Approve plan?\nType CONFIRM-0123ABCD to confirm: " {
				t.Fatalf("terminal output = %q", ttyOutput.String())
			}
		})
	}
}

func TestRandomChallengeUsesRandomSource(t *testing.T) {
	challenge, err := randomChallenge(bytes.NewReader([]byte{0x01, 0x23, 0xab, 0xcd}))
	if err != nil {
		t.Fatalf("randomChallenge() error = %v", err)
	}
	if challenge != "CONFIRM-0123ABCD" {
		t.Fatalf("randomChallenge() = %q", challenge)
	}
}

func TestRandomChallengePropagatesEntropyFailure(t *testing.T) {
	_, err := randomChallenge(errorReader{})
	if err == nil {
		t.Fatal("randomChallenge() error = nil")
	}
}

func TestCloseIsIdempotentAndStopsInteraction(t *testing.T) {
	closeCalls := 0
	terminal := newTerminal(strings.NewReader("reason\n"), io.Discard, func() error {
		closeCalls++
		return nil
	})

	if err := terminal.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := terminal.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", closeCalls)
	}
	if _, err := terminal.ReadReason("Reason: "); !errors.Is(err, ErrClosed) {
		t.Fatalf("ReadReason() after close error = %v, want ErrClosed", err)
	}
}

func TestValidateEndpointRejectsRegularFile(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "terminal-regular-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	err = validateEndpoint("input", file)
	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("validateEndpoint() error = %v, want ErrNotInteractive", err)
	}
}

func TestValidateEndpointRejectsNonTerminalCharacterDevice(t *testing.T) {
	file, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	err = validateEndpoint("input", file)
	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("validateEndpoint() error = %v, want ErrNotInteractive", err)
	}
}

func TestOpenWithUsesDevTTYForSeparateInputAndOutput(t *testing.T) {
	var flags []int
	openFile := func(name string, flag int, _ os.FileMode) (*os.File, error) {
		if name != controllingTTY {
			t.Fatalf("open path = %q, want %q", name, controllingTTY)
		}
		flags = append(flags, flag)
		return os.OpenFile(os.DevNull, os.O_RDWR, 0)
	}

	terminal, err := openWith(openFile)
	if terminal != nil {
		_ = terminal.Close()
		t.Fatal("openWith() terminal != nil for non-terminal character device")
	}
	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("openWith() error = %v, want ErrNotInteractive", err)
	}
	if len(flags) != 2 || flags[0] != os.O_RDONLY || flags[1] != os.O_WRONLY {
		t.Fatalf("open flags = %v, want [%d %d]", flags, os.O_RDONLY, os.O_WRONLY)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}
