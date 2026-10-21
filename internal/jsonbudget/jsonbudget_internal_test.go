package jsonbudget

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckStructureMarkersAndEmptyContainers(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		markers int
	}{
		{name: "empty array", json: `[]`, markers: 2},
		{name: "null values", json: `[null,null]`, markers: 3},
		{name: "empty strings", json: `["",""]`, markers: 3},
		{name: "empty objects", json: `[{},{}]`, markers: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Check([]byte(test.json), testLimits(test.markers, 128, 1024, 1024)); err != nil {
				t.Fatalf("exact marker limit rejected: %v", err)
			}
			if err := Check([]byte(test.json), testLimits(test.markers-1, 128, 1024, 1024)); !errors.Is(err, ErrLimit) {
				t.Fatalf("marker limit error = %v, want ErrLimit", err)
			}
		})
	}
	large := "[" + strings.Repeat("{},", 20) + "{}]"
	if err := Check([]byte(large), testLimits(10, 128, 1024, 1024)); err == nil {
		t.Fatal("many empty objects exceeded no marker budget")
	}
}

func TestCheckDepthExactAndAbove(t *testing.T) {
	value := strings.Repeat("[", 4) + `null` + strings.Repeat("]", 4)
	if err := Check([]byte(value), testLimits(20, 4, 1024, 1024)); err != nil {
		t.Fatalf("exact depth limit rejected: %v", err)
	}
	if err := Check([]byte(value), testLimits(20, 3, 1024, 1024)); err == nil {
		t.Fatal("depth limit one below input was accepted")
	}
}

func TestCheckStringBudgetAndEscapedStructuralMarkers(t *testing.T) {
	value := []byte(`{"x":"fake { [ ] , : } quote \" then slash \\","y":[]}`)
	if err := Check(value, testLimits(7, 2, 1024, 1024)); err != nil {
		t.Fatalf("structural markers inside strings counted: %v", err)
	}
	if err := Check(value, testLimits(6, 2, 1024, 1024)); err == nil {
		t.Fatal("structural markers outside strings were not counted")
	}
	stringValue := []byte(`{"a":"1234"}`)
	if err := Check(stringValue, testLimits(5, 1, 5, 1024)); err != nil {
		t.Fatalf("exact aggregate string limit rejected: %v", err)
	}
	if err := Check(stringValue, testLimits(5, 1, 4, 1024)); err == nil {
		t.Fatal("string limit one byte below input was accepted")
	}
	escapedString := []byte(`{"a":"\\\""}`)
	if err := Check(escapedString, testLimits(3, 1, 5, 1024)); err != nil {
		t.Fatalf("exact escaped string budget rejected: %v", err)
	}
	if err := Check(escapedString, testLimits(3, 1, 4, 1024)); err == nil {
		t.Fatal("escaped string bytes were not included in the budget")
	}
}

func TestCheckLiteralBudgetExactAndAbove(t *testing.T) {
	if err := Check([]byte(`1234`), testLimits(0, 0, 0, 4)); err != nil {
		t.Fatalf("exact literal limit rejected: %v", err)
	}
	if err := Check([]byte(`1234`), testLimits(0, 0, 0, 3)); err == nil {
		t.Fatal("literal limit one byte below input was accepted")
	}
}

func TestCheckBackslashParity(t *testing.T) {
	for _, value := range []string{
		`{"x":"quoted \" { } [ ] , : marker","y":null}`,
		`{"x":"two slashes \\ close","y":[]}`,
	} {
		if err := Check([]byte(value), testLimits(20, 2, 1024, 1024)); err != nil {
			t.Fatalf("valid escaped JSON rejected: %v: %s", err, value)
		}
	}
}

func TestCheckRejectsInvalidJSONAndUsesDefaultLimits(t *testing.T) {
	if err := Check([]byte(`{"x":`), DefaultLimits()); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("invalid JSON error = %v, want ErrInvalidJSON", err)
	}
	if DefaultLimits().MaxStructuralMarkers != 100_000 {
		t.Fatal("unexpected default marker limit")
	}
}

func testLimits(markers, depth, stringBytes, literalBytes int) Limits {
	return Limits{
		MaxStructuralMarkers: markers,
		MaxDepth:             depth,
		MaxStringBytes:       stringBytes,
		MaxLiteralBytes:      literalBytes,
	}
}
