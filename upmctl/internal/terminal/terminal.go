package terminal

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	controllingTTY = "/dev/tty"
	maxInputBytes  = 4096
)

var (
	ErrClosed         = errors.New("terminal: closed")
	ErrEmptyChallenge = errors.New("terminal: challenge is required")
	ErrEmptyReason    = errors.New("terminal: reason is required")
	ErrInputClosed    = errors.New("terminal: input closed")
	ErrNotInteractive = errors.New("terminal: endpoint is not an interactive terminal")
)

// HumanTerminal is the interactive surface required by human-only control
// operations. Implementations used by production must come from Open; callers
// can inject a fake implementation in tests.
type HumanTerminal interface {
	ReadReason(prompt string) (string, error)
	ConfirmChallenge(prompt, challenge string) (bool, error)
	Close() error
}

// Terminal reads from and writes to a controlling terminal. It never uses
// process stdin, stdout, or stderr implicitly.
type Terminal struct {
	mu      sync.Mutex
	scanner *bufio.Scanner
	writer  io.Writer
	close   func() error
	closed  bool
}

// Open opens the process controlling terminal explicitly. Both the input and
// output endpoints must be character devices that accept terminal ioctls.
func Open() (*Terminal, error) {
	return openWith(os.OpenFile)
}

// New constructs an injectable terminal without inspecting the supplied
// endpoints. It is intended for tests; production callers should use Open.
func New(reader io.Reader, writer io.Writer) *Terminal {
	return newTerminal(reader, writer, func() error { return nil })
}

func newTerminal(reader io.Reader, writer io.Writer, closeFunc func() error) *Terminal {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 256), maxInputBytes)
	return &Terminal{
		scanner: scanner,
		writer:  writer,
		close:   closeFunc,
	}
}

type openFileFunc func(string, int, os.FileMode) (*os.File, error)

func openWith(openFile openFileFunc) (*Terminal, error) {
	input, err := openFile(controllingTTY, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("terminal: open %s for input: %w", controllingTTY, err)
	}

	output, err := openFile(controllingTTY, os.O_WRONLY, 0)
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("terminal: open %s for output: %w", controllingTTY, err)
	}

	closeBoth := func() error {
		return errors.Join(input.Close(), output.Close())
	}
	if err := validateEndpoint("input", input); err != nil {
		_ = closeBoth()
		return nil, err
	}
	if err := validateEndpoint("output", output); err != nil {
		_ = closeBoth()
		return nil, err
	}

	return newTerminal(input, output, closeBoth), nil
}

func validateEndpoint(name string, file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("terminal: inspect %s endpoint: %w", name, err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("terminal: %s endpoint is not a character device: %w", name, ErrNotInteractive)
	}
	if !isInteractive(file.Fd()) {
		return fmt.Errorf("terminal: %s endpoint does not support terminal interaction: %w", name, ErrNotInteractive)
	}
	return nil
}

// ReadReason writes prompt to the terminal and reads one non-empty line. The
// returned reason is trimmed at its edges; blank input is rejected.
func (t *Terminal) ReadReason(prompt string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	line, err := t.readLine(prompt)
	if err != nil {
		return "", err
	}
	reason := strings.TrimSpace(line)
	if reason == "" {
		return "", ErrEmptyReason
	}
	return reason, nil
}

// ConfirmChallenge writes prompt and challenge to the terminal, then requires
// an exact, case-sensitive response. Only the line terminator is discarded.
func (t *Terminal) ConfirmChallenge(prompt, challenge string) (bool, error) {
	if challenge == "" {
		return false, ErrEmptyChallenge
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	line, err := t.readLine(fmt.Sprintf("%s\nType %s to confirm: ", prompt, challenge))
	if err != nil {
		return false, err
	}
	return line == challenge, nil
}

func (t *Terminal) readLine(prompt string) (string, error) {
	if t.closed {
		return "", ErrClosed
	}
	if _, err := io.WriteString(t.writer, prompt); err != nil {
		return "", fmt.Errorf("terminal: write prompt: %w", err)
	}
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return "", fmt.Errorf("terminal: read input: %w", err)
		}
		return "", ErrInputClosed
	}
	return t.scanner.Text(), nil
}

// RandomChallenge returns a cryptographically random, human-readable token
// suitable for ConfirmChallenge.
func RandomChallenge() (string, error) {
	return randomChallenge(rand.Reader)
}

func randomChallenge(source io.Reader) (string, error) {
	var raw [4]byte
	if _, err := io.ReadFull(source, raw[:]); err != nil {
		return "", fmt.Errorf("terminal: generate challenge: %w", err)
	}
	return "CONFIRM-" + strings.ToUpper(hex.EncodeToString(raw[:])), nil
}

// Close releases production terminal file descriptors. It is safe to call
// more than once.
func (t *Terminal) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.close == nil {
		return nil
	}
	return t.close()
}
