package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const (
	storeDirectoryMode = 0o700
	storeFileMode      = 0o600
	maxStoredPlanSize  = 1 << 20
)

var (
	storePlanIDPattern = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)

	// ErrInvalidPlanID reports a plan ID that cannot safely identify a plan
	// file in the store.
	ErrInvalidPlanID = errors.New("invalid plan ID")
	// ErrPlanExists reports an immutable plan ID collision. Existing plans are
	// never replaced, even when their contents appear identical.
	ErrPlanExists = errors.New("plan already exists")
	// ErrUnsafeStore reports a store path whose identity or permissions cannot
	// be trusted.
	ErrUnsafeStore = errors.New("unsafe plan store")
)

// Store persists immutable actionable plans below a single workspace.
type Store struct {
	workspace string
	initErr   error
}

// NewStore constructs a workspace-scoped plan store. It does not create or
// modify any files until Save is called.
func NewStore(workspace string) *Store {
	if workspace == "" {
		return &Store{initErr: fmt.Errorf("%w: workspace is empty", ErrUnsafeStore)}
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return &Store{initErr: fmt.Errorf("%w: resolve workspace: %v", ErrUnsafeStore, err)}
	}
	return &Store{workspace: filepath.Clean(absolute)}
}

// Save validates and atomically publishes an ACTION_REQUIRED plan. The
// publication uses a hard link so an existing plan can never be overwritten.
func (s *Store) Save(plan Plan) (string, error) {
	if s == nil {
		return "", fmt.Errorf("%w: store is nil", ErrUnsafeStore)
	}
	if s.initErr != nil {
		return "", s.initErr
	}
	if plan.Disposition != "ACTION_REQUIRED" {
		return "", fmt.Errorf("plan %q is not ACTION_REQUIRED", plan.PlanID)
	}
	if err := validatePlanID(plan.PlanID); err != nil {
		return "", err
	}
	if err := Validate(plan); err != nil {
		return "", fmt.Errorf("validate plan before saving: %w", err)
	}

	contents, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode plan %q: %w", plan.PlanID, err)
	}
	contents = append(contents, '\n')
	if len(contents) > maxStoredPlanSize {
		return "", fmt.Errorf("plan %q exceeds maximum stored size", plan.PlanID)
	}

	plansDirectory, err := s.plansDirectory(true)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(plansDirectory, plan.PlanID+".json")
	if err := ensureDestinationAbsent(destination); err != nil {
		return "", err
	}

	temporary, err := os.CreateTemp(plansDirectory, "."+plan.PlanID+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create exclusive temporary plan: %w", err)
	}
	temporaryPath := temporary.Name()
	temporaryClosed := false
	defer func() {
		if !temporaryClosed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryPath)
	}()

	if err := temporary.Chmod(storeFileMode); err != nil {
		return "", fmt.Errorf("set temporary plan permissions: %w", err)
	}
	if err := writeAll(temporary, contents); err != nil {
		return "", fmt.Errorf("write temporary plan: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("sync temporary plan: %w", err)
	}
	if err := temporary.Close(); err != nil {
		temporaryClosed = true
		return "", fmt.Errorf("close temporary plan: %w", err)
	}
	temporaryClosed = true

	// Decode the exact bytes that will be published before making them visible.
	if _, err := readPlanFile(temporaryPath, plan.PlanID); err != nil {
		return "", fmt.Errorf("validate temporary plan: %w", err)
	}
	if _, err := s.plansDirectory(false); err != nil {
		return "", err
	}
	if err := ensureDestinationAbsent(destination); err != nil {
		return "", err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s", ErrPlanExists, plan.PlanID)
		}
		return "", fmt.Errorf("publish plan %q without overwrite: %w", plan.PlanID, err)
	}

	if err := os.Remove(temporaryPath); err != nil {
		return "", fmt.Errorf("plan %q was published but temporary link cleanup failed: %w", plan.PlanID, err)
	}
	if err := syncDirectory(plansDirectory); err != nil {
		return "", fmt.Errorf("sync plan directory: %w", err)
	}
	if _, err := s.Read(plan.PlanID); err != nil {
		// Only remove the file if the directory still has the trusted identity.
		if _, safetyErr := s.plansDirectory(false); safetyErr == nil {
			_ = os.Remove(destination)
			_ = syncDirectory(plansDirectory)
		}
		return "", fmt.Errorf("read back published plan %q: %w", plan.PlanID, err)
	}
	return destination, nil
}

// Read loads and validates an immutable plan from the workspace store.
func (s *Store) Read(planID string) (Plan, error) {
	var zero Plan
	if s == nil {
		return zero, fmt.Errorf("%w: store is nil", ErrUnsafeStore)
	}
	if s.initErr != nil {
		return zero, s.initErr
	}
	if err := validatePlanID(planID); err != nil {
		return zero, err
	}
	plansDirectory, err := s.plansDirectory(false)
	if err != nil {
		return zero, err
	}
	loaded, err := readPlanFile(filepath.Join(plansDirectory, planID+".json"), planID)
	if err != nil {
		return zero, err
	}
	return loaded, nil
}

func validatePlanID(planID string) error {
	if !storePlanIDPattern.MatchString(planID) {
		return fmt.Errorf("%w: %q", ErrInvalidPlanID, planID)
	}
	return nil
}

func (s *Store) plansDirectory(create bool) (string, error) {
	workspaceInfo, err := os.Stat(s.workspace)
	if err != nil {
		return "", fmt.Errorf("%w: inspect workspace: %v", ErrUnsafeStore, err)
	}
	if !workspaceInfo.IsDir() {
		return "", fmt.Errorf("%w: workspace is not a directory", ErrUnsafeStore)
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(s.workspace)
	if err != nil {
		return "", fmt.Errorf("%w: resolve workspace identity: %v", ErrUnsafeStore, err)
	}
	resolvedWorkspace = filepath.Clean(resolvedWorkspace)

	upmctlDirectory := filepath.Join(s.workspace, ".upmctl")
	if err := ensurePrivateDirectory(upmctlDirectory, create); err != nil {
		return "", err
	}
	plansDirectory := filepath.Join(upmctlDirectory, "plans")
	if err := ensurePrivateDirectory(plansDirectory, create); err != nil {
		return "", err
	}

	resolvedUPMCTL, err := filepath.EvalSymlinks(upmctlDirectory)
	if err != nil {
		return "", fmt.Errorf("%w: resolve .upmctl identity: %v", ErrUnsafeStore, err)
	}
	resolvedPlans, err := filepath.EvalSymlinks(plansDirectory)
	if err != nil {
		return "", fmt.Errorf("%w: resolve plans identity: %v", ErrUnsafeStore, err)
	}
	if filepath.Clean(resolvedUPMCTL) != filepath.Join(resolvedWorkspace, ".upmctl") ||
		filepath.Clean(resolvedPlans) != filepath.Join(resolvedWorkspace, ".upmctl", "plans") {
		return "", fmt.Errorf("%w: plan store escapes workspace", ErrUnsafeStore)
	}
	// Recheck final path components after resolution to reject replacement by a
	// symlink between the earlier identity checks.
	if err := verifyPrivateDirectory(upmctlDirectory); err != nil {
		return "", err
	}
	if err := verifyPrivateDirectory(plansDirectory); err != nil {
		return "", err
	}
	if create {
		// Persist both directory entries as well as the plan file entry that Save
		// syncs after publication.
		if err := syncDirectory(resolvedWorkspace); err != nil {
			return "", fmt.Errorf("sync workspace directory: %w", err)
		}
		if err := syncDirectory(upmctlDirectory); err != nil {
			return "", fmt.Errorf("sync .upmctl directory: %w", err)
		}
	}
	return plansDirectory, nil
}

func ensurePrivateDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := os.Mkdir(path, storeDirectoryMode); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create private directory %s: %w", path, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !create {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		return fmt.Errorf("%w: inspect %s: %v", ErrUnsafeStore, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafeStore, path)
	}
	if create && info.Mode().Perm() != storeDirectoryMode {
		if err := os.Chmod(path, storeDirectoryMode); err != nil {
			return fmt.Errorf("set private directory permissions %s: %w", path, err)
		}
	}
	return verifyPrivateDirectory(path)
}

func verifyPrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: inspect %s: %v", ErrUnsafeStore, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: %s is not a real directory", ErrUnsafeStore, path)
	}
	if info.Mode().Perm() != storeDirectoryMode {
		return fmt.Errorf("%w: %s permissions are %04o, want %04o", ErrUnsafeStore, path, info.Mode().Perm(), storeDirectoryMode)
	}
	return nil
}

func ensureDestinationAbsent(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return fmt.Errorf("%w: %s", ErrPlanExists, filepath.Base(path))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect plan destination: %w", err)
	}
	return nil
}

func readPlanFile(path, expectedPlanID string) (Plan, error) {
	var plan Plan
	info, err := os.Lstat(path)
	if err != nil {
		return plan, fmt.Errorf("inspect plan file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return plan, fmt.Errorf("%w: plan file is not a real regular file", ErrUnsafeStore)
	}
	if info.Mode().Perm() != storeFileMode {
		return plan, fmt.Errorf("%w: plan file permissions are %04o, want %04o", ErrUnsafeStore, info.Mode().Perm(), storeFileMode)
	}
	if info.Size() > maxStoredPlanSize {
		return plan, fmt.Errorf("stored plan exceeds maximum size")
	}

	file, err := os.Open(path)
	if err != nil {
		return plan, fmt.Errorf("open plan file: %w", err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return plan, fmt.Errorf("inspect opened plan file: %w", err)
	}
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
		return plan, fmt.Errorf("%w: plan file identity changed while opening", ErrUnsafeStore)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxStoredPlanSize+1))
	if err != nil {
		return plan, fmt.Errorf("read plan file: %w", err)
	}
	if len(contents) > maxStoredPlanSize {
		return plan, fmt.Errorf("stored plan exceeds maximum size")
	}
	if err := rejectDuplicateObjectKeys(contents); err != nil {
		return plan, fmt.Errorf("decode plan JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return Plan{}, fmt.Errorf("decode plan JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Plan{}, errors.New("decode plan JSON: trailing JSON value")
		}
		return Plan{}, fmt.Errorf("decode plan JSON trailing data: %w", err)
	}
	if plan.PlanID != expectedPlanID {
		return Plan{}, fmt.Errorf("stored plan ID %q does not match requested ID %q", plan.PlanID, expectedPlanID)
	}
	if plan.Disposition != "ACTION_REQUIRED" {
		return Plan{}, fmt.Errorf("stored plan %q is not ACTION_REQUIRED", plan.PlanID)
	}
	if err := Validate(plan); err != nil {
		return Plan{}, fmt.Errorf("validate stored plan: %w", err)
	}
	return plan, nil
}

func rejectDuplicateObjectKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
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
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%w: sync target is not a real directory", ErrUnsafeStore)
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
	if !os.SameFile(info, openedInfo) {
		return fmt.Errorf("%w: directory identity changed while opening", ErrUnsafeStore)
	}
	return directory.Sync()
}
