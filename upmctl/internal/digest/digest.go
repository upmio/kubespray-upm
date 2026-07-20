// Package digest produces stable SHA-256 digests for JSON semantic values.
//
// Callers are responsible for constructing the semantic value to hash. Fields
// that are intentionally volatile, such as observation timestamps or request
// identifiers, should be omitted from that value before calling Sum.
package digest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const algorithmPrefix = "sha256:"

// CanonicalJSON returns a compact, deterministic JSON representation of value.
// JSON object keys are sorted lexicographically by encoding/json, while array
// order is preserved. Values that cannot be represented as JSON are rejected.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("digest: encode value as JSON: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()

	var semanticValue any
	if err := decoder.Decode(&semanticValue); err != nil {
		return nil, fmt.Errorf("digest: decode JSON value: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("digest: JSON value contains trailing data")
		}
		return nil, fmt.Errorf("digest: validate JSON value: %w", err)
	}

	canonical, err := json.Marshal(semanticValue)
	if err != nil {
		return nil, fmt.Errorf("digest: encode canonical JSON: %w", err)
	}
	return canonical, nil
}

// Sum returns the SHA-256 digest of value's canonical JSON representation.
// The returned value always has the form sha256:<64 lowercase hexadecimal
// characters>.
func Sum(value any) (string, error) {
	canonical, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(canonical)
	return algorithmPrefix + hex.EncodeToString(sum[:]), nil
}
