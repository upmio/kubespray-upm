package digest

import (
	"encoding/json"
	"math"
	"regexp"
	"testing"
)

func TestCanonicalJSONSortsMapKeys(t *testing.T) {
	t.Parallel()

	first := map[string]any{
		"z": 1,
		"nested": map[string]any{
			"b": true,
			"a": false,
		},
		"a": "first",
	}
	second := map[string]any{}
	second["a"] = "first"
	second["nested"] = map[string]any{"a": false, "b": true}
	second["z"] = 1

	firstJSON, err := CanonicalJSON(first)
	if err != nil {
		t.Fatalf("CanonicalJSON(first) error = %v", err)
	}
	secondJSON, err := CanonicalJSON(second)
	if err != nil {
		t.Fatalf("CanonicalJSON(second) error = %v", err)
	}

	const want = `{"a":"first","nested":{"a":false,"b":true},"z":1}`
	if string(firstJSON) != want {
		t.Fatalf("CanonicalJSON(first) = %s, want %s", firstJSON, want)
	}
	if string(secondJSON) != want {
		t.Fatalf("CanonicalJSON(second) = %s, want %s", secondJSON, want)
	}

	firstDigest, err := Sum(first)
	if err != nil {
		t.Fatalf("Sum(first) error = %v", err)
	}
	secondDigest, err := Sum(second)
	if err != nil {
		t.Fatalf("Sum(second) error = %v", err)
	}
	if firstDigest != secondDigest {
		t.Fatalf("map insertion order changed digest: %q != %q", firstDigest, secondDigest)
	}
	if matched := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(firstDigest); !matched {
		t.Fatalf("Sum(first) = %q, want sha256:<64 lowercase hex>", firstDigest)
	}
}

func TestCanonicalJSONPreservesSliceOrder(t *testing.T) {
	t.Parallel()

	forward, err := CanonicalJSON([]string{"k8s-1", "k8s-2", "k8s-3"})
	if err != nil {
		t.Fatalf("CanonicalJSON(forward) error = %v", err)
	}
	reverse, err := CanonicalJSON([]string{"k8s-3", "k8s-2", "k8s-1"})
	if err != nil {
		t.Fatalf("CanonicalJSON(reverse) error = %v", err)
	}

	if string(forward) != `["k8s-1","k8s-2","k8s-3"]` {
		t.Fatalf("CanonicalJSON(forward) = %s", forward)
	}
	if string(reverse) != `["k8s-3","k8s-2","k8s-1"]` {
		t.Fatalf("CanonicalJSON(reverse) = %s", reverse)
	}

	forwardDigest, err := Sum(json.RawMessage(forward))
	if err != nil {
		t.Fatalf("Sum(forward) error = %v", err)
	}
	reverseDigest, err := Sum(json.RawMessage(reverse))
	if err != nil {
		t.Fatalf("Sum(reverse) error = %v", err)
	}
	if forwardDigest == reverseDigest {
		t.Fatal("different slice order produced the same digest")
	}
}

func TestSumChangesWhenValueChanges(t *testing.T) {
	t.Parallel()

	before, err := Sum(map[string]any{"node": "k8s-3", "state": "stopped"})
	if err != nil {
		t.Fatalf("Sum(before) error = %v", err)
	}
	after, err := Sum(map[string]any{"node": "k8s-3", "state": "running"})
	if err != nil {
		t.Fatalf("Sum(after) error = %v", err)
	}
	if before == after {
		t.Fatal("value change produced the same digest")
	}
}

func TestCanonicalJSONRejectsNonJSONValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
	}{
		{name: "NaN", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
		{name: "function", value: func() {}},
		{name: "complex", value: complex(1, 2)},
		{name: "channel", value: make(chan struct{})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := CanonicalJSON(test.value); err == nil {
				t.Fatalf("CanonicalJSON(%s) error = nil, want rejection", test.name)
			}
			if got, err := Sum(test.value); err == nil || got != "" {
				t.Fatalf("Sum(%s) = %q, %v; want empty digest and error", test.name, got, err)
			}
		})
	}
}
