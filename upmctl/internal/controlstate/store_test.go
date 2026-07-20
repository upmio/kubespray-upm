package controlstate

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const testMaxSize int64 = 1024

func TestPublishReadAndList(t *testing.T) {
	workspace := t.TempDir()
	store := New(workspace)
	want := []byte(`{"kind":"Approval"}` + "\n")

	path, err := store.Publish("approvals/pending", "approval-002.json", want, testMaxSize)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	wantPath := filepath.Join(workspace, ".upmctl", "approvals", "pending", "approval-002.json")
	if path != wantPath {
		t.Fatalf("Publish() path = %q, want %q", path, wantPath)
	}
	for _, directory := range []string{
		filepath.Join(workspace, ".upmctl"),
		filepath.Join(workspace, ".upmctl", "approvals"),
		filepath.Join(workspace, ".upmctl", "approvals", "pending"),
	} {
		assertPermissions(t, directory, directoryMode)
	}
	assertPermissions(t, path, fileMode)

	got, err := store.Read("approvals/pending", "approval-002.json", testMaxSize)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Read() = %q, want %q", got, want)
	}
	if _, err := store.Publish("approvals/pending", "approval-001.json", []byte("one"), testMaxSize); err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
	names, err := store.List("approvals/pending")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	wantNames := []string{"approval-001.json", "approval-002.json"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Fatalf("List() = %v, want %v", names, wantNames)
	}
}

func TestInvalidPathsAreRejectedBeforeFilesystemAccess(t *testing.T) {
	invalidDirectories := []string{"", ".", "..", "/absolute", "a/", "/a", "a//b", "a/./b", "a/../b", `a\b`, "a/\x00b", "a/\x1fb", "a/\u0085b"}
	invalidFilenames := []string{"", ".", "..", "a/b", `a\b`, "\x00", "a\x7fb", "a\u0085b"}

	for _, relativeDir := range invalidDirectories {
		t.Run("directory_"+safeTestName(relativeDir), func(t *testing.T) {
			workspace := t.TempDir()
			store := New(workspace)
			if _, err := store.Publish(relativeDir, "artifact.json", []byte("x"), 1); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("Publish(%q) error = %v, want ErrUnsafe", relativeDir, err)
			}
			assertControlStateAbsent(t, workspace)
		})
	}
	for _, filename := range invalidFilenames {
		t.Run("filename_"+safeTestName(filename), func(t *testing.T) {
			workspace := t.TempDir()
			store := New(workspace)
			if _, err := store.Publish("approvals", filename, []byte("x"), 1); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("Publish(%q) error = %v, want ErrUnsafe", filename, err)
			}
			assertControlStateAbsent(t, workspace)
		})
	}

	workspace := t.TempDir()
	if _, err := New(workspace).Publish("approvals", "a.json", []byte("xx"), 1); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("oversized Publish() error = %v, want ErrUnsafe", err)
	}
	assertControlStateAbsent(t, workspace)
}

func TestPublishCollisionNeverOverwrites(t *testing.T) {
	workspace := t.TempDir()
	store := New(workspace)
	path, err := store.Publish("claims", "claim.json", []byte("first"), testMaxSize)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish("claims", "claim.json", []byte("second"), testMaxSize); !errors.Is(err, ErrExists) {
		t.Fatalf("second Publish() error = %v, want ErrExists", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "first" {
		t.Fatalf("collision overwrote immutable artifact with %q", contents)
	}
}

func TestConcurrentPublishSucceedsExactlyOnce(t *testing.T) {
	workspace := t.TempDir()
	store := New(workspace)
	start := make(chan struct{})
	errorsByCall := make([]error, 8)
	var wait sync.WaitGroup
	for index := range errorsByCall {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = store.Publish("claims", "claim.json", []byte("immutable"), testMaxSize)
		}(index)
	}
	close(start)
	wait.Wait()

	succeeded, collided := 0, 0
	for _, err := range errorsByCall {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrExists):
			collided++
		default:
			t.Fatalf("concurrent Publish() unexpected error = %v", err)
		}
	}
	if succeeded != 1 || collided != len(errorsByCall)-1 {
		t.Fatalf("concurrent results succeeded=%d collided=%d", succeeded, collided)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".upmctl", "claims"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "claim.json" {
		t.Fatalf("directory contains publication residue: %v", entryNames(entries))
	}
}

func TestSymlinkAndEscapeAttemptsAreRejected(t *testing.T) {
	t.Run("workspace", func(t *testing.T) {
		outside := t.TempDir()
		parent := t.TempDir()
		workspace := filepath.Join(parent, "workspace")
		if err := os.Symlink(outside, workspace); err != nil {
			t.Fatal(err)
		}
		if _, err := New(workspace).Publish("claims", "claim.json", []byte("x"), testMaxSize); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Publish() error = %v, want ErrUnsafe", err)
		}
	})

	t.Run("control directory", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, ".upmctl")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(workspace).Publish("claims", "claim.json", []byte("x"), testMaxSize); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Publish() error = %v, want ErrUnsafe", err)
		}
		assertPathAbsent(t, filepath.Join(outside, "claims"))
	})

	t.Run("nested directory", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		upmctl := filepath.Join(workspace, ".upmctl")
		if err := os.Mkdir(upmctl, directoryMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(upmctl, "claims")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(workspace).Publish("claims", "claim.json", []byte("x"), testMaxSize); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Publish() error = %v, want ErrUnsafe", err)
		}
		assertPathAbsent(t, filepath.Join(outside, "claim.json"))
	})

	t.Run("artifact", func(t *testing.T) {
		workspace := t.TempDir()
		directory := createPrivatePath(t, workspace, "claims")
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("outside"), fileMode); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(directory, "claim.json")); err != nil {
			t.Fatal(err)
		}
		store := New(workspace)
		if _, err := store.Read("claims", "claim.json", testMaxSize); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Read() error = %v, want ErrUnsafe", err)
		}
		if _, err := store.Publish("claims", "claim.json", []byte("new"), testMaxSize); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("Publish() error = %v, want ErrUnsafe", err)
		}
	})
}

func TestTamperingAndPermissionAnomaliesRejectWholeOperation(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(t *testing.T, workspace, directory, artifact string)
	}{
		{
			name: "artifact permissions",
			tamper: func(t *testing.T, _, _, artifact string) {
				if err := os.Chmod(artifact, 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "artifact replaced by directory",
			tamper: func(t *testing.T, _, _, artifact string) {
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(artifact, directoryMode); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory permissions",
			tamper: func(t *testing.T, _, directory, _ string) {
				if err := os.Chmod(directory, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			store := New(workspace)
			artifact, err := store.Publish("claims", "claim.json", []byte("trusted"), testMaxSize)
			if err != nil {
				t.Fatal(err)
			}
			directory := filepath.Dir(artifact)
			test.tamper(t, workspace, directory, artifact)
			if _, err := store.Read("claims", "claim.json", testMaxSize); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("Read() error = %v, want ErrUnsafe", err)
			}
			if _, err := store.List("claims"); !errors.Is(err, ErrUnsafe) {
				t.Fatalf("List() error = %v, want ErrUnsafe", err)
			}
		})
	}
}

func TestListRejectsAnyUnsafeEntryWithoutPartialResults(t *testing.T) {
	workspace := t.TempDir()
	store := New(workspace)
	if _, err := store.Publish("operations", "safe.json", []byte("safe"), testMaxSize); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(workspace, ".upmctl", "operations")
	if err := os.WriteFile(filepath.Join(directory, "unsafe.json"), []byte("unsafe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List("operations"); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("List() error = %v, want ErrUnsafe", err)
	}
}

func TestReadRejectsOversizedStoredArtifact(t *testing.T) {
	workspace := t.TempDir()
	store := New(workspace)
	if _, err := store.Publish("operations", "large.json", bytes.Repeat([]byte("x"), 32), 64); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("operations", "large.json", 16); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("Read() error = %v, want ErrUnsafe", err)
	}
}

func TestReadDetectsFileIdentitySwitch(t *testing.T) {
	workspace := t.TempDir()
	directory := createPrivatePath(t, workspace, "claims")
	path := filepath.Join(directory, "claim.json")
	if err := os.WriteFile(path, []byte("original"), fileMode); err != nil {
		t.Fatal(err)
	}
	inspected, err := inspectSafeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".replaced"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), fileMode); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readSafeFileAfterInspect(path, testMaxSize, inspected); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("readSafeFileAfterInspect() error = %v, want ErrUnsafe", err)
	}
}

func createPrivatePath(t *testing.T, workspace, relativeDir string) string {
	t.Helper()
	current := filepath.Join(workspace, ".upmctl")
	if err := os.Mkdir(current, directoryMode); err != nil {
		t.Fatal(err)
	}
	for _, segment := range strings.Split(relativeDir, "/") {
		current = filepath.Join(current, segment)
		if err := os.Mkdir(current, directoryMode); err != nil {
			t.Fatal(err)
		}
	}
	return current
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s permissions = %04o, want %04o", path, got, want)
	}
}

func assertControlStateAbsent(t *testing.T, workspace string) {
	t.Helper()
	assertPathAbsent(t, filepath.Join(workspace, ".upmctl"))
}

func assertPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or returned unexpected error: %v", path, err)
	}
}

func safeTestName(value string) string {
	replacer := strings.NewReplacer("/", "_slash_", `\`, "_backslash_", "\x00", "_nul_", "\x1f", "_control_", "\x7f", "_delete_")
	if value == "" {
		return "empty"
	}
	return replacer.Replace(value)
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
