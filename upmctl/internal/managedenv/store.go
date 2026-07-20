package managedenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxStateSize int64 = 1 << 20

type Store struct {
	workspace string
}

func NewStore(workspace string) *Store { return &Store{workspace: workspace} }

// Save atomically publishes .upmctl/state.json without replacing any existing
// state. The only persistent write is the managed identity itself.
func (s *Store) Save(state State) (string, error) {
	root, err := realWorkspace(s.workspace)
	if err != nil {
		return "", failure(FailureStoreUnsafe, "%v", err)
	}
	if state.Workspace != root {
		return "", failure(FailureStoreUnsafe, "managed state workspace does not match the canonical workspace")
	}
	if err := state.Validate(); err != nil {
		return "", failure(FailureStoreUnsafe, "%v", err)
	}
	if err := requireEmptyControlState(root); err != nil {
		return "", err
	}
	contents, err := encodeState(state)
	if err != nil {
		return "", err
	}

	directoryPath := filepath.Join(root, ".upmctl")
	createdDirectory := false
	if err := os.Mkdir(directoryPath, 0o700); err == nil {
		createdDirectory = true
		if err := os.Chmod(directoryPath, 0o700); err != nil {
			return "", failure(FailureStoreUnsafe, "set .upmctl permissions: %v", err)
		}
		if err := syncDirectory(root); err != nil {
			return "", failure(FailureStoreUnsafe, "sync workspace after creating .upmctl: %v", err)
		}
	} else if !errors.Is(err, os.ErrExist) {
		return "", failure(FailureStoreUnsafe, "create .upmctl: %v", err)
	}
	cleanupDirectory := func() {
		if createdDirectory {
			_ = os.Remove(directoryPath)
			_ = syncDirectory(root)
		}
	}
	published := false
	defer func() {
		if !published {
			cleanupDirectory()
		}
	}()
	info, err := os.Lstat(directoryPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return "", failure(FailureStoreUnsafe, ".upmctl is not a private real directory with mode 0700")
	}

	temporary, err := os.CreateTemp(directoryPath, ".managed-environment-*.tmp")
	if err != nil {
		return "", failure(FailureStoreUnsafe, "create managed state temporary file: %v", err)
	}
	temporaryPath := temporary.Name()
	temporaryClosed := false
	defer func() {
		if !temporaryClosed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", failure(FailureStoreUnsafe, "set managed state temporary permissions: %v", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		return "", failure(FailureStoreUnsafe, "write managed state temporary file: %v", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", failure(FailureStoreUnsafe, "sync managed state temporary file: %v", err)
	}
	if err := temporary.Close(); err != nil {
		temporaryClosed = true
		return "", failure(FailureStoreUnsafe, "close managed state temporary file: %v", err)
	}
	temporaryClosed = true
	temporaryInfo, err := os.Lstat(temporaryPath)
	if err != nil || !temporaryInfo.Mode().IsRegular() || temporaryInfo.Mode().Perm() != 0o600 {
		return "", failure(FailureStoreUnsafe, "managed state temporary identity is unsafe")
	}
	if err := ensureNoControlArtifacts(directoryPath); err != nil {
		return "", err
	}

	destination := filepath.Join(directoryPath, "state.json")
	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", failure(FailureStateExists, "managed state already exists")
		}
		return "", failure(FailureStoreUnsafe, "publish managed state without overwrite: %v", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return "", failure(FailureStoreUnsafe, "managed state published but temporary cleanup failed: %v", err)
	}
	if err := syncDirectory(directoryPath); err != nil {
		return "", failure(FailureStoreUnsafe, "sync managed state directory: %v", err)
	}
	readBack, err := readStateFile(destination)
	if err != nil || !bytes.Equal(readBack, contents) {
		if destinationInfo, inspectErr := os.Lstat(destination); inspectErr == nil && os.SameFile(temporaryInfo, destinationInfo) && destinationInfo.Mode().IsRegular() && destinationInfo.Mode().Perm() == 0o600 {
			_ = os.Remove(destination)
			_ = syncDirectory(directoryPath)
		}
		return "", failure(FailureStoreUnsafe, "published managed state failed safe readback")
	}
	published = true
	return destination, nil
}

// Rollback removes state.json only when it is still the exact private artifact
// encoded from state. It is used when strict post-publication validation fails;
// it never removes a replaced file or unrelated control-state.
func (s *Store) Rollback(state State) error {
	root, err := realWorkspace(s.workspace)
	if err != nil || state.Workspace != root {
		return failure(FailureStoreUnsafe, "rollback workspace identity is unsafe")
	}
	contents, err := encodeState(state)
	if err != nil {
		return err
	}
	directory := filepath.Join(root, ".upmctl")
	directoryInfo, err := os.Lstat(directory)
	if err != nil || directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		return failure(FailureStoreUnsafe, "rollback control directory is unsafe")
	}
	destination := filepath.Join(directory, "state.json")
	fileInfo, err := os.Lstat(destination)
	if err != nil || fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0o600 {
		return failure(FailureStoreUnsafe, "rollback managed state identity is unsafe")
	}
	current, err := readStateFile(destination)
	if err != nil || !bytes.Equal(current, contents) {
		return failure(FailureStoreUnsafe, "rollback refused because managed state changed")
	}
	finalInfo, err := os.Lstat(destination)
	if err != nil || !os.SameFile(fileInfo, finalInfo) {
		return failure(FailureStoreUnsafe, "rollback refused because managed state identity changed")
	}
	if err := os.Remove(destination); err != nil {
		return failure(FailureStoreUnsafe, "remove rejected managed state: %v", err)
	}
	if err := syncDirectory(directory); err != nil {
		return failure(FailureStoreUnsafe, "sync managed state rollback: %v", err)
	}
	if entries, readErr := os.ReadDir(directory); readErr == nil && len(entries) == 0 {
		if currentDirectory, statErr := os.Lstat(directory); statErr == nil && os.SameFile(directoryInfo, currentDirectory) {
			_ = os.Remove(directory)
			_ = syncDirectory(root)
		}
	}
	return nil
}

func encodeState(state State) ([]byte, error) {
	if err := state.Validate(); err != nil {
		return nil, failure(FailureStoreUnsafe, "%v", err)
	}
	contents, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, failure(FailureStoreUnsafe, "marshal managed state: %v", err)
	}
	contents = append(contents, '\n')
	if int64(len(contents)) > maxStateSize {
		return nil, failure(FailureStoreUnsafe, "managed state exceeds maximum size")
	}
	return contents, nil
}

func ensureNoControlArtifacts(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return failure(FailureStoreUnsafe, "read .upmctl before publication: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".managed-environment-") && strings.HasSuffix(entry.Name(), ".tmp") && entry.Type().IsRegular() {
			continue
		}
		return failure(FailureAlreadyControlled, "workspace acquired upmctl state or control-state during adoption")
	}
	return nil
}

func readStateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > maxStateSize {
		return nil, fmt.Errorf("managed state is not a private bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("managed state identity changed")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxStateSize+1))
	if err != nil || int64(len(contents)) > maxStateSize {
		return nil, fmt.Errorf("managed state read failed")
	}
	return contents, nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
