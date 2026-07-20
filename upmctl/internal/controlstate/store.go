// Package controlstate provides a workspace-scoped store for immutable control
// artifacts. It deliberately stores opaque bytes; schema and domain validation
// remain the responsibility of the calling package.
package controlstate

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	directoryMode = 0o700
	fileMode      = 0o600
)

var (
	// ErrUnsafe reports an untrusted path, file, directory, permission, or
	// argument. Callers must not retry an operation that returns ErrUnsafe
	// without first repairing or re-observing the control-state directory.
	ErrUnsafe = errors.New("unsafe control state")
	// ErrExists reports an immutable artifact collision. Publish never replaces
	// an existing artifact.
	ErrExists = errors.New("control artifact already exists")
)

// Store persists immutable control artifacts below a workspace's .upmctl
// directory.
type Store struct {
	workspace string
	initErr   error
}

// New constructs a workspace-scoped Store. It does not create control-state
// directories until Publish is called.
func New(workspace string) *Store {
	if workspace == "" {
		return &Store{initErr: fmt.Errorf("%w: workspace is empty", ErrUnsafe)}
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return &Store{initErr: fmt.Errorf("%w: resolve workspace: %v", ErrUnsafe, err)}
	}
	return &Store{workspace: filepath.Clean(absolute)}
}

// Publish atomically creates filename below relativeDir. It uses an exclusive
// temporary file and a hard-link publication, so an existing artifact is never
// overwritten. maxSize must be positive.
func (s *Store) Publish(relativeDir, filename string, contents []byte, maxSize int64) (string, error) {
	if err := s.validateOperation(relativeDir, filename, maxSize); err != nil {
		return "", err
	}
	if int64(len(contents)) > maxSize {
		return "", fmt.Errorf("%w: artifact size %d exceeds maximum %d", ErrUnsafe, len(contents), maxSize)
	}

	directory, err := s.directory(relativeDir, true)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(directory, filename)
	if err := ensureDestinationAbsent(destination); err != nil {
		return "", err
	}

	temporary, err := os.CreateTemp(directory, ".controlstate-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create exclusive temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryClosed := false
	defer func() {
		if !temporaryClosed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(fileMode); err != nil {
		return "", fmt.Errorf("set temporary artifact permissions: %w", err)
	}
	if err := writeAll(temporary, contents); err != nil {
		return "", fmt.Errorf("write temporary artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporaryClosed = true
		return "", fmt.Errorf("close temporary artifact: %w", err)
	}
	temporaryClosed = true

	written, temporaryInfo, err := readSafeFile(temporaryPath, maxSize)
	if err != nil {
		return "", fmt.Errorf("validate temporary artifact: %w", err)
	}
	if !bytes.Equal(written, contents) {
		return "", fmt.Errorf("%w: temporary artifact contents changed", ErrUnsafe)
	}
	if _, err := s.directory(relativeDir, false); err != nil {
		return "", err
	}
	if err := ensureDestinationAbsent(destination); err != nil {
		return "", err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s", ErrExists, filename)
		}
		return "", fmt.Errorf("publish artifact %q without overwrite: %w", filename, err)
	}

	if err := os.Remove(temporaryPath); err != nil {
		return "", fmt.Errorf("artifact %q was published but temporary link cleanup failed: %w", filename, err)
	}
	if err := syncDirectory(directory); err != nil {
		return "", fmt.Errorf("sync artifact directory: %w", err)
	}
	readBack, err := s.Read(relativeDir, filename, maxSize)
	if err != nil || !bytes.Equal(readBack, contents) {
		// Remove only when the destination is still a safe artifact. A replaced
		// destination is left untouched and reported as unsafe.
		if destinationInfo, infoErr := inspectSafeFile(destination); infoErr == nil && os.SameFile(temporaryInfo, destinationInfo) {
			_ = os.Remove(destination)
			_ = syncDirectory(directory)
		}
		if err != nil {
			return "", fmt.Errorf("read back published artifact %q: %w", filename, err)
		}
		return "", fmt.Errorf("%w: published artifact %q changed during readback", ErrUnsafe, filename)
	}
	return destination, nil
}

// Read returns the exact bytes of a safely stored immutable artifact.
func (s *Store) Read(relativeDir, filename string, maxSize int64) ([]byte, error) {
	if err := s.validateOperation(relativeDir, filename, maxSize); err != nil {
		return nil, err
	}
	directory, err := s.directory(relativeDir, false)
	if err != nil {
		return nil, err
	}
	artifactPath := filepath.Join(directory, filename)
	contents, fileInfo, err := readSafeFile(artifactPath, maxSize)
	if err != nil {
		return nil, err
	}
	if _, err := s.directory(relativeDir, false); err != nil {
		return nil, err
	}
	currentInfo, err := inspectSafeFile(artifactPath)
	if err != nil || !os.SameFile(fileInfo, currentInfo) {
		return nil, fmt.Errorf("%w: control artifact identity changed after reading", ErrUnsafe)
	}
	return contents, nil
}

// List returns safe artifact filenames in bytewise lexical order. The entire
// operation fails if any directory entry is unsafe; callers never receive a
// partial view that silently omits tampered state.
func (s *Store) List(relativeDir string) ([]string, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}
	if err := validateRelativeDirectory(relativeDir); err != nil {
		return nil, err
	}
	directoryPath, err := s.directory(relativeDir, false)
	if err != nil {
		return nil, err
	}
	directory, directoryInfo, err := openPrivateDirectory(directoryPath)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("read control-state directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if err := validateName(name, "filename"); err != nil {
			return nil, err
		}
		if err := verifySafeFileIdentity(filepath.Join(directoryPath, name)); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	currentInfo, err := os.Lstat(directoryPath)
	if err != nil || !os.SameFile(directoryInfo, currentInfo) {
		return nil, fmt.Errorf("%w: directory identity changed while listing", ErrUnsafe)
	}
	sort.Strings(names)
	return names, nil
}

func (s *Store) validateOperation(relativeDir, filename string, maxSize int64) error {
	if err := s.validateStore(); err != nil {
		return err
	}
	if err := validateRelativeDirectory(relativeDir); err != nil {
		return err
	}
	if err := validateName(filename, "filename"); err != nil {
		return err
	}
	if maxSize <= 0 || maxSize == int64(^uint64(0)>>1) {
		return fmt.Errorf("%w: maximum size must be positive", ErrUnsafe)
	}
	return nil
}

func (s *Store) validateStore() error {
	if s == nil {
		return fmt.Errorf("%w: store is nil", ErrUnsafe)
	}
	return s.initErr
}

func validateRelativeDirectory(relativeDir string) error {
	if relativeDir == "" || filepath.IsAbs(relativeDir) || strings.Contains(relativeDir, `\`) {
		return fmt.Errorf("%w: invalid relative directory %q", ErrUnsafe, relativeDir)
	}
	for _, segment := range strings.Split(relativeDir, "/") {
		if err := validateName(segment, "directory segment"); err != nil {
			return err
		}
	}
	return nil
}

func validateName(name, kind string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid %s %q", ErrUnsafe, kind, name)
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return fmt.Errorf("%w: invalid %s %q", ErrUnsafe, kind, name)
		}
	}
	return nil
}

func (s *Store) directory(relativeDir string, create bool) (string, error) {
	workspaceInfo, err := os.Lstat(s.workspace)
	if err != nil {
		return "", fmt.Errorf("%w: inspect workspace: %v", ErrUnsafe, err)
	}
	if workspaceInfo.Mode()&os.ModeSymlink != 0 || !workspaceInfo.IsDir() {
		return "", fmt.Errorf("%w: workspace is not a real directory", ErrUnsafe)
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(s.workspace)
	if err != nil {
		return "", fmt.Errorf("%w: resolve workspace identity: %v", ErrUnsafe, err)
	}
	resolvedWorkspace = filepath.Clean(resolvedWorkspace)

	current := s.workspace
	segments := append([]string{".upmctl"}, strings.Split(relativeDir, "/")...)
	for _, segment := range segments {
		parent := current
		current = filepath.Join(current, segment)
		created, err := ensurePrivateDirectory(current, create)
		if err != nil {
			return "", err
		}
		if created {
			var syncErr error
			if parent == s.workspace {
				syncErr = syncWorkspaceDirectory(parent)
			} else {
				syncErr = syncDirectory(parent)
			}
			if syncErr != nil {
				return "", fmt.Errorf("sync parent directory after creating %s: %w", current, syncErr)
			}
		}
	}

	resolvedDirectory, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", fmt.Errorf("%w: resolve control-state identity: %v", ErrUnsafe, err)
	}
	want := filepath.Join(append([]string{resolvedWorkspace, ".upmctl"}, strings.Split(relativeDir, "/")...)...)
	if filepath.Clean(resolvedDirectory) != filepath.Clean(want) {
		return "", fmt.Errorf("%w: control-state path escapes workspace", ErrUnsafe)
	}
	for path := current; path != s.workspace; path = filepath.Dir(path) {
		if err := verifyPrivateDirectory(path); err != nil {
			return "", err
		}
	}
	currentWorkspaceInfo, err := os.Lstat(s.workspace)
	if err != nil || !os.SameFile(workspaceInfo, currentWorkspaceInfo) {
		return "", fmt.Errorf("%w: workspace identity changed", ErrUnsafe)
	}
	return current, nil
}

func ensurePrivateDirectory(path string, create bool) (bool, error) {
	info, err := os.Lstat(path)
	created := false
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, directoryMode); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return false, fmt.Errorf("create private directory %s: %w", path, err)
			}
		} else {
			created = true
			if err := os.Chmod(path, directoryMode); err != nil {
				return false, fmt.Errorf("set private directory permissions %s: %w", path, err)
			}
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return false, fmt.Errorf("%w: inspect %s: %v", ErrUnsafe, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != directoryMode {
		return false, fmt.Errorf("%w: %s is not a private real directory with mode %04o", ErrUnsafe, path, directoryMode)
	}
	return created, verifyPrivateDirectory(path)
}

func verifyPrivateDirectory(path string) error {
	directory, _, err := openPrivateDirectory(path)
	if directory != nil {
		_ = directory.Close()
	}
	return err
}

func openPrivateDirectory(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: inspect %s: %v", ErrUnsafe, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != directoryMode {
		return nil, nil, fmt.Errorf("%w: %s is not a private real directory with mode %04o", ErrUnsafe, path, directoryMode)
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open control-state directory: %w", err)
	}
	openedInfo, err := directory.Stat()
	if err != nil {
		_ = directory.Close()
		return nil, nil, fmt.Errorf("inspect opened control-state directory: %w", err)
	}
	currentInfo, currentErr := os.Lstat(path)
	if !os.SameFile(info, openedInfo) || currentErr != nil || !os.SameFile(openedInfo, currentInfo) ||
		!currentInfo.IsDir() || currentInfo.Mode().Perm() != directoryMode {
		_ = directory.Close()
		return nil, nil, fmt.Errorf("%w: directory identity changed while opening", ErrUnsafe)
	}
	return directory, openedInfo, nil
}

func ensureDestinationAbsent(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect artifact destination: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != fileMode {
		return fmt.Errorf("%w: existing destination is not a safe artifact", ErrUnsafe)
	}
	return fmt.Errorf("%w: %s", ErrExists, filepath.Base(path))
}

func inspectSafeFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect control artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: control artifact is not a real regular file", ErrUnsafe)
	}
	if info.Mode().Perm() != fileMode {
		return nil, fmt.Errorf("%w: control artifact permissions are %04o, want %04o", ErrUnsafe, info.Mode().Perm(), fileMode)
	}
	return info, nil
}

func verifySafeFileIdentity(path string) error {
	info, err := inspectSafeFile(path)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open control artifact: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened control artifact: %w", err)
	}
	currentInfo, currentErr := os.Lstat(path)
	if !os.SameFile(info, openedInfo) || currentErr != nil || !os.SameFile(openedInfo, currentInfo) ||
		!openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != fileMode ||
		!currentInfo.Mode().IsRegular() || currentInfo.Mode().Perm() != fileMode {
		return fmt.Errorf("%w: control artifact identity changed while opening", ErrUnsafe)
	}
	return nil
}

func readSafeFile(path string, maxSize int64) ([]byte, os.FileInfo, error) {
	info, err := inspectSafeFile(path)
	if err != nil {
		return nil, nil, err
	}
	return readSafeFileAfterInspect(path, maxSize, info)
}

func readSafeFileAfterInspect(path string, maxSize int64, info os.FileInfo) ([]byte, os.FileInfo, error) {
	if info.Size() > maxSize {
		return nil, nil, fmt.Errorf("%w: stored artifact exceeds maximum size", ErrUnsafe)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open control artifact: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect opened control artifact: %w", err)
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != fileMode {
		return nil, nil, fmt.Errorf("%w: control artifact identity changed while opening", ErrUnsafe)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read control artifact: %w", err)
	}
	if int64(len(contents)) > maxSize {
		return nil, nil, fmt.Errorf("%w: stored artifact exceeds maximum size", ErrUnsafe)
	}
	finalOpenedInfo, statErr := file.Stat()
	currentInfo, err := os.Lstat(path)
	if statErr != nil || err != nil || !os.SameFile(openedInfo, finalOpenedInfo) || !os.SameFile(finalOpenedInfo, currentInfo) ||
		!finalOpenedInfo.Mode().IsRegular() || finalOpenedInfo.Mode().Perm() != fileMode ||
		!currentInfo.Mode().IsRegular() || currentInfo.Mode().Perm() != fileMode ||
		finalOpenedInfo.Size() != int64(len(contents)) {
		return nil, nil, fmt.Errorf("%w: control artifact identity changed while reading", ErrUnsafe)
	}
	return contents, openedInfo, nil
}

func writeAll(file *os.File, contents []byte) error {
	for len(contents) > 0 {
		written, err := file.Write(contents)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		contents = contents[written:]
	}
	return nil
}

func syncDirectory(path string) error {
	directory, _, err := openPrivateDirectory(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return err
	}
	return nil
}

func syncWorkspaceDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: workspace sync target is not a real directory", ErrUnsafe)
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	openedInfo, err := directory.Stat()
	if err != nil {
		return err
	}
	currentInfo, currentErr := os.Lstat(path)
	if !os.SameFile(info, openedInfo) || currentErr != nil || !os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("%w: workspace identity changed while opening", ErrUnsafe)
	}
	return directory.Sync()
}
