package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/upmio/kubespray-upm/upmctl/internal/controlstate"
)

const (
	approvalDirectory     = "approvals/by-plan"
	maxStoredApprovalSize = int64(1 << 20)
)

var (
	storePlanIDPattern     = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)
	storeApprovalIDPattern = regexp.MustCompile(`^approval-[0-9a-f]{64}$`)

	ErrInvalidPlanID     = errors.New("invalid plan ID")
	ErrInvalidApprovalID = errors.New("invalid approval ID")
	ErrApprovalExists    = errors.New("approval already exists")
	ErrApprovalNotFound  = errors.New("approval not found")
)

// Store persists one immutable Approval per Plan below
// .upmctl/approvals/by-plan. The Plan ID is the storage uniqueness boundary;
// Approval IDs are resolved through validated, stable store enumeration.
type Store struct {
	state     *controlstate.Store
	workspace string
	initErr   error
}

// NewStore constructs a workspace-scoped Approval store. It does not create
// control-state directories until Save is called.
func NewStore(workspace string) *Store {
	result := &Store{state: controlstate.New(workspace)}
	if workspace == "" {
		result.initErr = fmt.Errorf("%w: workspace is empty", controlstate.ErrUnsafe)
		return result
	}
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		result.initErr = fmt.Errorf("%w: resolve workspace: %v", controlstate.ErrUnsafe, err)
		return result
	}
	result.workspace = filepath.Clean(absolute)
	return result
}

// Save validates and atomically publishes an Approval. An existing Approval
// for the same Plan is never overwritten, renewed, or replaced.
func (s *Store) Save(value Approval) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	if err := validateStorePlanID(value.PlanID); err != nil {
		return "", err
	}
	if err := validateStoreApprovalID(value.ApprovalID); err != nil {
		return "", err
	}
	if err := ValidateIntegrity(value); err != nil {
		return "", fmt.Errorf("validate approval before saving: %w", err)
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode approval %q: %w", value.ApprovalID, err)
	}
	contents = append(contents, '\n')
	if int64(len(contents)) > maxStoredApprovalSize {
		return "", fmt.Errorf("approval %q exceeds maximum stored size", value.ApprovalID)
	}
	decoded, err := decodeApproval(contents)
	if err != nil {
		return "", fmt.Errorf("validate encoded approval %q: %w", value.ApprovalID, err)
	}
	if decoded.PlanID != value.PlanID || decoded.ApprovalID != value.ApprovalID {
		return "", errors.New("encoded approval identity changed")
	}
	path, err := s.state.Publish(approvalDirectory, value.PlanID+".json", contents, maxStoredApprovalSize)
	if errors.Is(err, controlstate.ErrExists) {
		return "", fmt.Errorf("%w: %s", ErrApprovalExists, value.PlanID)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// ReadByPlan loads the immutable Approval stored for planID and verifies that
// both its domain integrity and its storage-key binding are intact.
func (s *Store) ReadByPlan(planID string) (Approval, error) {
	var zero Approval
	if err := s.ready(); err != nil {
		return zero, err
	}
	if err := validateStorePlanID(planID); err != nil {
		return zero, err
	}
	contents, err := s.state.Read(approvalDirectory, planID+".json", maxStoredApprovalSize)
	if err != nil {
		if s.safelyMissing(approvalDirectory, planID+".json") {
			return zero, fmt.Errorf("%w: %s", ErrApprovalNotFound, planID)
		}
		return zero, err
	}
	value, err := decodeApproval(contents)
	if err != nil {
		return zero, err
	}
	if value.PlanID != planID {
		return zero, fmt.Errorf("stored approval planId %q does not match storage key %q", value.PlanID, planID)
	}
	return value, nil
}

// GetByApprovalID resolves an Approval ID through a strict, all-or-nothing
// store scan. Unsafe or invalid entries fail the whole lookup.
func (s *Store) GetByApprovalID(approvalID string) (Approval, error) {
	var zero Approval
	if err := validateStoreApprovalID(approvalID); err != nil {
		return zero, err
	}
	values, err := s.List()
	if err != nil {
		return zero, err
	}
	for _, value := range values {
		if value.ApprovalID == approvalID {
			return value, nil
		}
	}
	return zero, fmt.Errorf("%w: %s", ErrApprovalNotFound, approvalID)
}

// List returns Approvals in ascending Plan ID order. With no argument it
// returns every Approval. With one Plan ID it returns either that one Approval
// or ErrApprovalNotFound. More than one filter is rejected.
func (s *Store) List(planID ...string) ([]Approval, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if len(planID) > 1 {
		return nil, errors.New("approval list accepts at most one plan ID filter")
	}
	if len(planID) == 1 {
		value, err := s.ReadByPlan(planID[0])
		if err != nil {
			return nil, err
		}
		return []Approval{value}, nil
	}

	names, err := s.state.List(approvalDirectory)
	if err != nil {
		if s.safelyMissingDirectory(approvalDirectory) {
			return []Approval{}, nil
		}
		return nil, err
	}
	values := make([]Approval, 0, len(names))
	for _, name := range names {
		if filepath.Ext(name) != ".json" {
			return nil, fmt.Errorf("unexpected approval store entry %q", name)
		}
		storedPlanID := strings.TrimSuffix(name, ".json")
		if err := validateStorePlanID(storedPlanID); err != nil {
			return nil, fmt.Errorf("invalid approval store entry %q: %w", name, err)
		}
		value, err := s.ReadByPlan(storedPlanID)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].PlanID < values[j].PlanID })
	return values, nil
}

func (s *Store) ready() error {
	if s == nil {
		return fmt.Errorf("%w: approval store is nil", controlstate.ErrUnsafe)
	}
	if s.initErr != nil {
		return s.initErr
	}
	return nil
}

func validateStorePlanID(planID string) error {
	if !storePlanIDPattern.MatchString(planID) {
		return fmt.Errorf("%w: %q", ErrInvalidPlanID, planID)
	}
	return nil
}

func validateStoreApprovalID(approvalID string) error {
	if !storeApprovalIDPattern.MatchString(approvalID) {
		return fmt.Errorf("%w: %q", ErrInvalidApprovalID, approvalID)
	}
	return nil
}

func decodeApproval(contents []byte) (Approval, error) {
	var value Approval
	if err := rejectDuplicateApprovalKeys(contents); err != nil {
		return value, fmt.Errorf("decode approval JSON: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return Approval{}, fmt.Errorf("decode approval JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Approval{}, errors.New("decode approval JSON: trailing JSON value")
		}
		return Approval{}, fmt.Errorf("decode approval JSON trailing data: %w", err)
	}
	if err := ValidateIntegrity(value); err != nil {
		return Approval{}, fmt.Errorf("validate stored approval: %w", err)
	}
	return value, nil
}

func rejectDuplicateApprovalKeys(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := walkApprovalJSONValue(decoder); err != nil {
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

func walkApprovalJSONValue(decoder *json.Decoder) error {
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
			if err := walkApprovalJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkApprovalJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func (s *Store) safelyMissing(relativeDir, filename string) bool {
	directory, missing := s.inspectStoreDirectory(relativeDir)
	if missing {
		return true
	}
	if directory == "" {
		return false
	}
	_, err := os.Lstat(filepath.Join(directory, filename))
	return errors.Is(err, os.ErrNotExist)
}

func (s *Store) safelyMissingDirectory(relativeDir string) bool {
	_, missing := s.inspectStoreDirectory(relativeDir)
	return missing
}

// inspectStoreDirectory distinguishes a genuinely absent control-state path
// from an unsafe one without following symlinks. Empty directory and false
// means the path exists but is not a trusted private directory.
func (s *Store) inspectStoreDirectory(relativeDir string) (directory string, missing bool) {
	workspaceInfo, err := os.Lstat(s.workspace)
	if err != nil || workspaceInfo.Mode()&os.ModeSymlink != 0 || !workspaceInfo.IsDir() {
		return "", false
	}
	current := s.workspace
	segments := append([]string{".upmctl"}, strings.Split(relativeDir, "/")...)
	for _, segment := range segments {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return "", true
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return "", false
		}
	}
	return current, false
}
