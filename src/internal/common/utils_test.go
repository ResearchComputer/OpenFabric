package common

import (
	"testing"
)

func TestDeduplicateStrings_NoDuplicates(t *testing.T) {
	input := []string{"a", "b", "c"}
	result := DeduplicateStrings(input)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	for i, v := range input {
		if result[i] != v {
			t.Fatalf("expected %q at index %d, got %q", v, i, result[i])
		}
	}
}

func TestDeduplicateStrings_WithDuplicates(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	result := DeduplicateStrings(input)
	expected := []string{"a", "b", "c"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d elements, got %d", len(expected), len(result))
	}
	for i, v := range expected {
		if result[i] != v {
			t.Fatalf("expected %q at index %d, got %q", v, i, result[i])
		}
	}
}

func TestDeduplicateStrings_AllSame(t *testing.T) {
	input := []string{"x", "x", "x", "x"}
	result := DeduplicateStrings(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	if result[0] != "x" {
		t.Fatalf("expected %q, got %q", "x", result[0])
	}
}

func TestDeduplicateStrings_EmptySlice(t *testing.T) {
	input := []string{}
	result := DeduplicateStrings(input)
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(result))
	}
}

func TestDeduplicateStrings_NilSlice(t *testing.T) {
	var input []string
	result := DeduplicateStrings(input)
	if result == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(result))
	}
}

func TestDeduplicateStrings_PreservesOrder(t *testing.T) {
	input := []string{"cherry", "apple", "banana", "apple", "cherry", "date"}
	result := DeduplicateStrings(input)
	expected := []string{"cherry", "apple", "banana", "date"}
	if len(result) != len(expected) {
		t.Fatalf("expected %d elements, got %d", len(expected), len(result))
	}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("index %d: expected %q, got %q", i, v, result[i])
		}
	}
}
