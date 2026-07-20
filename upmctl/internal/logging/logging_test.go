package logging

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoggerWritesPrivateJSONLLifecycle(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "upmctl.jsonl")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 17, 1, 2, 3, 456, time.FixedZone("test", 8*60*60))
	if err := logger.Start(now, "req-log", "approval grant"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Finish(now.Add(time.Second), "req-log", "approval grant", 3, "UPMCTL_BLOCKED"); err != nil {
		t.Fatal(err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL record %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Event != "start" || events[0].ExitCode != nil || events[1].Event != "error" || events[1].ExitCode == nil || *events[1].ExitCode != 3 || events[1].ErrorCode == nil || *events[1].ErrorCode != "UPMCTL_BLOCKED" {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Timestamp != "2026-07-16T17:02:03.000000456Z" {
		t.Fatalf("timestamp = %q", events[0].Timestamp)
	}
}

func TestOpenAppendsExistingPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upmctl.jsonl")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Start(time.Now(), "req", "version"); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(contents), "existing\n") {
		t.Fatalf("existing contents were overwritten: %q", contents)
	}
}

func TestOpenRejectsUnsafeTargets(t *testing.T) {
	directory := t.TempDir()
	worldReadable := filepath.Join(directory, "world-readable.log")
	if err := os.WriteFile(worldReadable, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(worldReadable); err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("world-readable error = %v", err)
	}

	if _, err := Open(filepath.Join(directory, "missing", "upmctl.log")); err == nil || !strings.Contains(err.Error(), "log directory") {
		t.Fatalf("missing directory error = %v", err)
	}

	if runtime.GOOS != "windows" {
		target := filepath.Join(directory, "target.log")
		if err := os.WriteFile(target, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "link.log")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(link); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("symlink error = %v", err)
		}
	}
}

func TestFinishWithoutErrorIsComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upmctl.jsonl")
	logger, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Finish(time.Now(), "req", "preflight", 3, ""); err != nil {
		t.Fatal(err)
	}
	_ = logger.Close()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(contents, &event); err != nil {
		t.Fatal(err)
	}
	if event.Event != "complete" || event.ExitCode == nil || *event.ExitCode != 3 || event.ErrorCode != nil {
		t.Fatalf("event = %#v", event)
	}
}
