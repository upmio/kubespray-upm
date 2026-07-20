// Package logging provides the optional, privacy-minimized CLI runtime log.
// It is deliberately independent from stdout/stderr command envelopes.
package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const logVersion = "upmctl.runtime/v1"

// Event is one JSONL runtime lifecycle record. ExitCode and ErrorCode are
// null for a start event; ErrorCode is also null for a successful completion.
type Event struct {
	LogVersion string  `json:"logVersion"`
	Timestamp  string  `json:"timestamp"`
	RequestID  string  `json:"requestId"`
	Command    string  `json:"command"`
	Event      string  `json:"event"`
	ExitCode   *int    `json:"exitCode"`
	ErrorCode  *string `json:"errorCode"`
}

// Logger appends complete JSON records to a private regular file.
type Logger struct {
	file *os.File
	mu   sync.Mutex
}

// Open opens path for append or creates it with mode 0600. The containing
// directory must already exist. Symlinks, non-regular files and existing files
// with permissions other than 0600 are rejected.
func Open(path string) (*Logger, error) {
	if path == "" {
		return nil, errors.New("log file path is empty")
	}
	clean := filepath.Clean(path)
	parent := filepath.Dir(clean)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return nil, fmt.Errorf("inspect log directory %q: %w", parent, err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return nil, fmt.Errorf("log directory %q must be an existing non-symlink directory", parent)
	}

	file, err := openPrivateRegular(clean)
	if err != nil {
		return nil, err
	}
	return &Logger{file: file}, nil
}

func openPrivateRegular(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	switch {
	case err == nil:
		if err := validateExisting(path, before); err != nil {
			return nil, err
		}
		return openVerifiedExisting(path, before)
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("inspect log file %q: %w", path, err)
	}

	file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL, 0o600)
	if createErr == nil {
		return file, nil
	}
	if errors.Is(createErr, os.ErrExist) {
		// Another process won creation. Re-run all identity and mode checks.
		current, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, fmt.Errorf("inspect concurrently created log file %q: %w", path, statErr)
		}
		if err := validateExisting(path, current); err != nil {
			return nil, err
		}
		return openVerifiedExisting(path, current)
	}
	return nil, fmt.Errorf("create log file %q: %w", path, createErr)
}

func validateExisting(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("log file %q must not be a symlink", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log file %q must be a regular file", path)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("existing log file %q must have permissions 0600, got %04o", path, info.Mode().Perm())
	}
	return nil
}

func openVerifiedExisting(path string, before os.FileInfo) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	opened, statErr := file.Stat()
	current, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || current.Mode()&os.ModeSymlink != 0 || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(current, opened) {
		_ = file.Close()
		return nil, fmt.Errorf("log file %q changed identity while opening", path)
	}
	if opened.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, fmt.Errorf("existing log file %q must have permissions 0600, got %04o", path, opened.Mode().Perm())
	}
	return file, nil
}

// Start records command admission into the CLI process.
func (l *Logger) Start(now time.Time, requestID, command string) error {
	return l.write(Event{
		LogVersion: logVersion,
		Timestamp:  now.UTC().Format(time.RFC3339Nano),
		RequestID:  requestID,
		Command:    command,
		Event:      "start",
	})
}

// Finish records either complete or error. A non-empty errorCode selects the
// error event; non-zero policy outcomes without an error envelope remain
// complete events with their real exit code.
func (l *Logger) Finish(now time.Time, requestID, command string, exitCode int, errorCode string) error {
	event := "complete"
	var code *string
	if errorCode != "" {
		event = "error"
		code = &errorCode
	}
	return l.write(Event{
		LogVersion: logVersion,
		Timestamp:  now.UTC().Format(time.RFC3339Nano),
		RequestID:  requestID,
		Command:    command,
		Event:      event,
		ExitCode:   &exitCode,
		ErrorCode:  code,
	})
}

func (l *Logger) write(event Event) error {
	if l == nil || l.file == nil {
		return errors.New("runtime logger is not open")
	}
	contents, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode runtime log event: %w", err)
	}
	contents = append(contents, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	written, err := l.file.Write(contents)
	if err != nil {
		return fmt.Errorf("append runtime log event: %w", err)
	}
	if written != len(contents) {
		return fmt.Errorf("append runtime log event: short write (%d of %d bytes)", written, len(contents))
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync runtime log event: %w", err)
	}
	return nil
}

// Close releases the runtime log file.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}
