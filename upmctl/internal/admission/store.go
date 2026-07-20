package admission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/upmio/kubespray-upm/upmctl/internal/controlstate"
)

const (
	admissionDirectory     = "admissions"
	maxStoredAdmissionSize = int64(1 << 20)
)

var (
	storePlanIDPattern = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)

	ErrInvalidPlanID       = errors.New("invalid plan ID")
	ErrAdmissionExists     = errors.New("admission already exists")
	ErrAdmissionNotFound   = errors.New("admission not found")
	ErrUnsupportedArtifact = errors.New("unsupported admission artifact")
)

// Store persists the one immutable admission decision slot for each Plan.
// Revocation and Claim deliberately compete for the same filename.
type Store struct {
	state     *controlstate.Store
	workspace string
	initErr   error
}

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

// Save atomically publishes a Revocation or Claim into the Plan's shared
// admission slot. Approval is part of the general Artifact decode union but is
// never a valid value for admissions/<planId>.json.
func (s *Store) Save(value Artifact) (string, error) {
	if err := s.ready(); err != nil {
		return "", err
	}
	kind, err := value.Kind()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedArtifact, err)
	}
	if kind != KindApprovalRevocation && kind != KindPlanClaim {
		return "", fmt.Errorf("%w: kind %q cannot occupy an admission slot", ErrUnsupportedArtifact, kind)
	}
	planID, err := value.PlanID()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsupportedArtifact, err)
	}
	if err := validateStorePlanID(planID); err != nil {
		return "", err
	}
	if err := value.ValidateIntegrity(); err != nil {
		return "", fmt.Errorf("validate admission before saving: %w", err)
	}
	contents, err := EncodeArtifact(value)
	if err != nil {
		return "", fmt.Errorf("encode admission for %q: %w", planID, err)
	}
	contents = append(contents, '\n')
	if int64(len(contents)) > maxStoredAdmissionSize {
		return "", fmt.Errorf("admission for %q exceeds maximum stored size", planID)
	}
	decoded, err := decodeStoredAdmission(contents, planID)
	if err != nil {
		return "", fmt.Errorf("validate encoded admission for %q: %w", planID, err)
	}
	decodedKind, _ := decoded.Kind()
	if decodedKind != kind {
		return "", errors.New("encoded admission kind changed")
	}
	path, err := s.state.Publish(admissionDirectory, planID+".json", contents, maxStoredAdmissionSize)
	if errors.Is(err, controlstate.ErrExists) {
		return "", fmt.Errorf("%w: %s", ErrAdmissionExists, planID)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// Read loads and validates the Revocation or Claim stored for planID.
func (s *Store) Read(planID string) (Artifact, error) {
	var zero Artifact
	if err := s.ready(); err != nil {
		return zero, err
	}
	if err := validateStorePlanID(planID); err != nil {
		return zero, err
	}
	contents, err := s.state.Read(admissionDirectory, planID+".json", maxStoredAdmissionSize)
	if err != nil {
		if s.safelyMissing(planID + ".json") {
			return zero, fmt.Errorf("%w: %s", ErrAdmissionNotFound, planID)
		}
		return zero, err
	}
	return decodeStoredAdmission(contents, planID)
}

func decodeStoredAdmission(contents []byte, expectedPlanID string) (Artifact, error) {
	value, err := DecodeArtifact(contents)
	if err != nil {
		return Artifact{}, err
	}
	kind, err := value.Kind()
	if err != nil {
		return Artifact{}, err
	}
	if kind != KindApprovalRevocation && kind != KindPlanClaim {
		return Artifact{}, fmt.Errorf("%w: kind %q cannot occupy an admission slot", ErrUnsupportedArtifact, kind)
	}
	planID, err := value.PlanID()
	if err != nil {
		return Artifact{}, err
	}
	if planID != expectedPlanID {
		return Artifact{}, fmt.Errorf("stored admission planId %q does not match storage key %q", planID, expectedPlanID)
	}
	return value, nil
}

func (s *Store) ready() error {
	if s == nil {
		return fmt.Errorf("%w: admission store is nil", controlstate.ErrUnsafe)
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

func (s *Store) safelyMissing(filename string) bool {
	workspaceInfo, err := os.Lstat(s.workspace)
	if err != nil || workspaceInfo.Mode()&os.ModeSymlink != 0 || !workspaceInfo.IsDir() {
		return false
	}
	current := s.workspace
	for _, segment := range []string{".upmctl", admissionDirectory} {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return true
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return false
		}
	}
	_, err = os.Lstat(filepath.Join(current, filename))
	return errors.Is(err, os.ErrNotExist)
}
