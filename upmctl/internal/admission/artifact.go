package admission

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
)

// Artifact is the closed union stored in admissions/<planId>.json. Exactly
// one member must be non-nil. Its JSON representation is the selected artifact
// itself, not a second envelope, so kind remains the discriminator.
type Artifact struct {
	Approval   *approval.Approval
	Revocation *ApprovalRevocation
	Claim      *PlanClaim
}

func ApprovalArtifact(value approval.Approval) Artifact {
	copy := value
	return Artifact{Approval: &copy}
}

func RevocationArtifact(value ApprovalRevocation) Artifact {
	copy := value
	return Artifact{Revocation: &copy}
}

func ClaimArtifact(value PlanClaim) Artifact {
	copy := value
	copy.LockFencing = cloneFencing(value.LockFencing)
	return Artifact{Claim: &copy}
}

func (a Artifact) Kind() (string, error) {
	switch a.count() {
	case 0:
		return "", errors.New("admission artifact is empty")
	case 1:
	default:
		return "", errors.New("admission artifact has multiple values")
	}
	if a.Approval != nil {
		return approval.Kind, nil
	}
	if a.Revocation != nil {
		return KindApprovalRevocation, nil
	}
	return KindPlanClaim, nil
}

func (a Artifact) PlanID() (string, error) {
	if _, err := a.Kind(); err != nil {
		return "", err
	}
	switch {
	case a.Approval != nil:
		return a.Approval.PlanID, nil
	case a.Revocation != nil:
		return a.Revocation.PlanID, nil
	default:
		return a.Claim.PlanID, nil
	}
}

func (a Artifact) ValidateIntegrity() error {
	if _, err := a.Kind(); err != nil {
		return err
	}
	switch {
	case a.Approval != nil:
		return approval.ValidateIntegrity(*a.Approval)
	case a.Revocation != nil:
		return ValidateApprovalRevocationIntegrity(*a.Revocation)
	default:
		return ValidatePlanClaimIntegrity(*a.Claim)
	}
}

func (a Artifact) count() int {
	count := 0
	if a.Approval != nil {
		count++
	}
	if a.Revocation != nil {
		count++
	}
	if a.Claim != nil {
		count++
	}
	return count
}

func (a Artifact) MarshalJSON() ([]byte, error) {
	if err := a.ValidateIntegrity(); err != nil {
		return nil, err
	}
	switch {
	case a.Approval != nil:
		return json.Marshal(a.Approval)
	case a.Revocation != nil:
		return json.Marshal(a.Revocation)
	default:
		return json.Marshal(a.Claim)
	}
}

func (a *Artifact) UnmarshalJSON(contents []byte) error {
	decoded, err := DecodeArtifact(contents)
	if err != nil {
		return err
	}
	*a = decoded
	return nil
}

// EncodeArtifact encodes a validated closed-union value as compact JSON.
func EncodeArtifact(value Artifact) ([]byte, error) {
	return json.Marshal(value)
}

// DecodeArtifact rejects duplicate keys, unknown fields, unsupported kinds,
// multiple JSON values, and semantically invalid/tampered artifacts.
func DecodeArtifact(contents []byte) (Artifact, error) {
	var result Artifact
	if err := rejectDuplicateObjectKeys(contents); err != nil {
		return result, fmt.Errorf("decode admission artifact: %w", err)
	}
	var discriminator struct {
		Kind string `json:"kind"`
	}
	if err := decodeStrict(contents, &discriminator, false); err != nil {
		return result, fmt.Errorf("decode admission artifact kind: %w", err)
	}
	switch discriminator.Kind {
	case approval.Kind:
		var value approval.Approval
		if err := decodeStrict(contents, &value, true); err != nil {
			return result, fmt.Errorf("decode Approval: %w", err)
		}
		result = ApprovalArtifact(value)
	case KindApprovalRevocation:
		var value ApprovalRevocation
		if err := decodeStrict(contents, &value, true); err != nil {
			return result, fmt.Errorf("decode ApprovalRevocation: %w", err)
		}
		result = RevocationArtifact(value)
	case KindPlanClaim:
		var value PlanClaim
		if err := decodeStrict(contents, &value, true); err != nil {
			return result, fmt.Errorf("decode PlanClaim: %w", err)
		}
		result = ClaimArtifact(value)
	case "":
		return result, errors.New("decode admission artifact: kind is required")
	default:
		return result, fmt.Errorf("decode admission artifact: unsupported kind %q", discriminator.Kind)
	}
	if err := result.ValidateIntegrity(); err != nil {
		return Artifact{}, fmt.Errorf("validate admission artifact: %w", err)
	}
	return result, nil
}

func decodeStrict(contents []byte, target any, disallowUnknown bool) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if disallowUnknown {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
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
