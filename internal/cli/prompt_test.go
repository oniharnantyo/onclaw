package cli

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestParseString(t *testing.T) {
	tests := []struct {
		input    string
		def      string
		expected string
		ok       bool
	}{
		{"hello", "", "hello", true},
		{"  hello  ", "", "hello", true},
		{"", "default", "default", true},
		{"   ", "default", "default", true},
		{"", "", "", false},
		{"   ", "", "", false},
	}

	for _, tc := range tests {
		val, ok := parseString(tc.input, tc.def)
		if val != tc.expected || ok != tc.ok {
			t.Errorf("parseString(%q, %q) = (%q, %t); expected (%q, %t)", tc.input, tc.def, val, ok, tc.expected, tc.ok)
		}
	}
}

func TestParseChoice(t *testing.T) {
	tests := []struct {
		input    string
		num      int
		expected int
		ok       bool
	}{
		{"1", 3, 0, true},
		{"3", 3, 2, true},
		{"0", 3, 0, false},
		{"4", 3, 0, false},
		{"abc", 3, 0, false},
		{"", 3, 0, false},
	}

	for _, tc := range tests {
		val, ok := parseChoice(tc.input, tc.num)
		if val != tc.expected || ok != tc.ok {
			t.Errorf("parseChoice(%q, %d) = (%d, %t); expected (%d, %t)", tc.input, tc.num, val, ok, tc.expected, tc.ok)
		}
	}
}

func TestParseConfirm(t *testing.T) {
	tests := []struct {
		input    string
		defYes   bool
		expected bool
		ok       bool
	}{
		{"y", false, true, true},
		{"YES", false, true, true},
		{"n", true, false, true},
		{"No", true, false, true},
		{"", true, true, true},
		{"", false, false, true},
		{"maybe", true, false, false},
	}

	for _, tc := range tests {
		val, ok := parseConfirm(tc.input, tc.defYes)
		if val != tc.expected || ok != tc.ok {
			t.Errorf("parseConfirm(%q, %t) = (%t, %t); expected (%t, %t)", tc.input, tc.defYes, val, ok, tc.expected, tc.ok)
		}
	}
}

func TestPromptString(t *testing.T) {
	// Case 1: valid input
	in := bytes.NewBufferString("user input\n")
	var out bytes.Buffer
	val, err := promptString("Enter something", "", in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "user input" {
		t.Errorf("expected 'user input', got %q", val)
	}
	if out.String() != "Enter something: " {
		t.Errorf("expected output 'Enter something: ', got %q", out.String())
	}

	// Case 2: default fallback on empty
	in = bytes.NewBufferString("\n")
	out.Reset()
	val, err = promptString("Enter value", "my-default", in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "my-default" {
		t.Errorf("expected 'my-default', got %q", val)
	}
	if out.String() != "Enter value [my-default]: " {
		t.Errorf("expected output 'Enter value [my-default]: ', got %q", out.String())
	}

	// Case 3: empty input re-prompts, then valid input
	in = bytes.NewBufferString("\n\nhello\n")
	out.Reset()
	val, err = promptString("Enter value", "", in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %q", val)
	}

	// Case 4: EOF
	in = bytes.NewBufferString("")
	out.Reset()
	_, err = promptString("Enter value", "", in, &out)
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestPromptSecret(t *testing.T) {
	// Piped input
	in := bytes.NewBufferString("my-secret\n")
	var out bytes.Buffer
	val, err := promptSecret("API Key", in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "my-secret" {
		t.Errorf("expected 'my-secret', got %q", val)
	}

	// Empty input re-prompt, then valid
	in = bytes.NewBufferString("\nmy-secret2\n")
	out.Reset()
	val, err = promptSecret("API Key", in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "my-secret2" {
		t.Errorf("expected 'my-secret2', got %q", val)
	}
}

func TestPromptChoice(t *testing.T) {
	choices := []string{"apple", "banana"}
	in := bytes.NewBufferString("invalid\n2\n")
	var out bytes.Buffer
	val, err := promptChoice("Select fruit", choices, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 1 {
		t.Errorf("expected index 1, got %d", val)
	}
}

func TestPromptConfirm(t *testing.T) {
	in := bytes.NewBufferString("invalid\ny\n")
	var out bytes.Buffer
	val, err := promptConfirm("Continue", false, in, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !val {
		t.Errorf("expected true, got false")
	}
}
