package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWriteEnvelopeProducesStableJSONFields(t *testing.T) {
	var buffer bytes.Buffer
	timestamp := time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC)
	err := WriteEnvelope(&buffer, JSON, Envelope{
		Kind:      "Test",
		RequestID: "req-test",
		Timestamp: timestamp,
		Data:      map[string]any{"ok": true},
	})
	if err != nil {
		t.Fatalf("WriteEnvelope() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if decoded["apiVersion"] != "upmctl.upm.io/v1alpha1" {
		t.Fatalf("apiVersion = %v", decoded["apiVersion"])
	}
	if decoded["kind"] != "Test" || decoded["requestId"] != "req-test" {
		t.Fatalf("unexpected envelope: %#v", decoded)
	}
}

func TestWriteErrorAlwaysIncludesContractFields(t *testing.T) {
	var buffer bytes.Buffer
	err := WriteError(&buffer, JSON, ErrorEnvelope{
		Kind:      "Error",
		RequestID: "req-error",
		Timestamp: time.Date(2026, 7, 17, 1, 2, 3, 0, time.UTC),
		Error: ErrorBody{
			Code:    "UPMCTL_TEST_ERROR",
			Message: "test failure",
		},
	})
	if err != nil {
		t.Fatalf("WriteError() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	body := decoded["error"].(map[string]any)
	if _, ok := body["details"]; !ok {
		t.Fatal("error.details is missing")
	}
	if _, ok := body["remediation"]; !ok {
		t.Fatal("error.remediation is missing")
	}
}

func TestContractSchemasAreValidJSON(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	schemas, err := filepath.Glob(filepath.Join(repository, "upmctl", "specs", "v1", "schemas", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) == 0 {
		t.Fatal("no contract schemas found")
	}
	for _, schema := range schemas {
		contents, err := os.ReadFile(schema)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(contents, &document); err != nil {
			t.Fatalf("schema %s is invalid JSON: %v", schema, err)
		}
		if document["$schema"] == nil || document["$id"] == nil {
			t.Fatalf("schema %s lacks $schema or $id", schema)
		}
	}
}
