package pattern

import (
	"encoding/json"
	"testing"
)

func TestJSON2Pattern(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "simple flat document",
			input:    map[string]any{"b": 1, "a": 2},
			expected: `{"a": 1, "b": 1}`,
		},
		{
			name: "nested document",
			input: map[string]any{
				"a": map[string]any{
					"b": 1,
					"c": []any{1, 2, 3},
				},
			},
			expected: `{"a": {"b": 1, "c": [1]}}`,
		},
		{
			name:     "empty document",
			input:    map[string]any{},
			expected: `{}`,
		},
		{
			name: "empty array in document",
			input: map[string]any{
				"a": []any{},
			},
			expected: `{"a": []}`,
		},
		{
			name: "complex nested with $or",
			input: map[string]any{
				"$or": []any{
					map[string]any{"x": 1},
					map[string]any{
						"$or": []any{
							map[string]any{"y": 1},
						},
					},
				},
			},
			expected: `{"$or": [{"x": 1}]}`,
		},
		{
			name: "deeply nested objects",
			input: map[string]any{
				"level1": map[string]any{
					"level2": map[string]any{
						"level3": "value",
					},
				},
			},
			expected: `{"level1": {"level2": {"level3": 1}}}`,
		},
		{
			name: "various value types in map",
			input: map[string]any{
				"string": "hello",
				"int":    42,
				"float":  3.1415,
				"bool":   true,
				"nil":    nil,
			},
			expected: `{"bool": 1, "float": 1, "int": 1, "nil": 1, "string": 1}`,
		},
		{
			name:     "primitive string",
			input:    "hello world",
			expected: `1`,
		},
		{
			name:     "primitive integer",
			input:    42,
			expected: `1`,
		},
		{
			name:     "primitive float",
			input:    3.14,
			expected: `1`,
		},
		{
			name:     "primitive boolean true",
			input:    true,
			expected: `1`,
		},
		{
			name:     "primitive boolean false",
			input:    false,
			expected: `1`,
		},
		{
			name:     "nil value",
			input:    nil,
			expected: `1`,
		},
		{
			name:     "empty slice at top level",
			input:    []any{},
			expected: `[]`,
		},
		{
			name:     "primitive slice at top level",
			input:    []any{1, 2, 3},
			expected: `[1]`,
		},
		{
			name: "slice of maps at top level",
			input: []any{
				map[string]any{"z": 10, "a": 20},
				map[string]any{"b": 30},
			},
			expected: `[{"a": 1, "z": 1}]`,
		},
		{
			name: "typed map (via reflection)",
			input: map[string]string{
				"b": "second",
				"a": "first",
			},
			expected: `{"a": 1, "b": 1}`,
		},
		{
			name:     "empty typed map (via reflection)",
			input:    map[string]int{},
			expected: `{}`,
		},
		{
			name:     "typed slice (via reflection)",
			input:    []int{10, 20, 30},
			expected: `[1]`,
		},
		{
			name:     "empty typed slice (via reflection)",
			input:    []string{},
			expected: `[]`,
		},
		{
			name:     "fixed array (via reflection)",
			input:    [2]int{1, 2},
			expected: `[1]`,
		},
		{
			name:     "empty fixed array (via reflection)",
			input:    [0]int{},
			expected: `[]`,
		},
		{
			name:     "pre-normalized sortedMap",
			input:    sortedMap{entry{key: "a", value: 1}},
			expected: `{"a": 1}`,
		},
		{
			name:     "pre-normalized patternList",
			input:    patternList{1},
			expected: `[1]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JSON2Pattern(tt.input)
			if got != tt.expected {
				t.Errorf("JSON2Pattern() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestJSON2PatternFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple flat JSON",
			input:    `{"b": "hello", "a": 42}`,
			expected: `{"a": 1, "b": 1}`,
		},
		{
			name:     "nested JSON",
			input:    `{"a": {"b": 1, "c": [1, 2, 3]}}`,
			expected: `{"a": {"b": 1, "c": [1]}}`,
		},
		{
			name:     "empty JSON object",
			input:    `{}`,
			expected: `{}`,
		},
		{
			name:     "empty JSON array in object",
			input:    `{"a": []}`,
			expected: `{"a": []}`,
		},
		{
			name:     "complex nested with $or",
			input:    `{"$or": [{"x": 1}, {"$or": [{"y": 1}]}]}`,
			expected: `{"$or": [{"x": 1}]}`,
		},
		{
			name:     "JSON null literal",
			input:    `null`,
			expected: `1`,
		},
		{
			name:     "JSON boolean true literal",
			input:    `true`,
			expected: `1`,
		},
		{
			name:     "JSON boolean false literal",
			input:    `false`,
			expected: `1`,
		},
		{
			name:     "JSON string literal",
			input:    `"hello"`,
			expected: `1`,
		},
		{
			name:     "JSON number literal",
			input:    `123`,
			expected: `1`,
		},
		{
			name:     "JSON top-level empty array",
			input:    `[]`,
			expected: `[]`,
		},
		{
			name:     "JSON top-level non-empty array",
			input:    `[1, 2, 3]`,
			expected: `[1]`,
		},
		{
			name:     "JSON with keys containing quotes or special characters",
			input:    `{"a/b": 1, "hello \"world\"": "val"}`,
			expected: `{"a/b": 1, "hello \"world\"": 1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JSON2PatternFromString(tt.input)
			if err != nil {
				t.Fatalf("JSON2PatternFromString(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("JSON2PatternFromString(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestJSON2PatternFromString_Errors(t *testing.T) {
	invalidInputs := []struct {
		name  string
		input string
	}{
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "whitespace only",
			input: "   \t\n",
		},
		{
			name:  "unquoted key",
			input: `{a: 1}`,
		},
		{
			name:  "unclosed brace",
			input: `{"a": 1`,
		},
		{
			name:  "unclosed bracket",
			input: `[1, 2`,
		},
		{
			name:  "trailing comma",
			input: `{"a": 1,}`,
		},
		{
			name:  "plain text word",
			input: `invalid`,
		},
	}

	for _, tt := range invalidInputs {
		t.Run(tt.name, func(t *testing.T) {
			got, err := JSON2PatternFromString(tt.input)
			if err == nil {
				t.Errorf("JSON2PatternFromString(%q) expected error, got result: %q", tt.input, got)
			}
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Run("sortedMap MarshalJSON", func(t *testing.T) {
		sm := sortedMap{
			entry{key: "a", value: 1},
			entry{key: "b", value: 1},
		}
		data, err := sm.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON() unexpected error: %v", err)
		}
		if string(data) != `{"a": 1, "b": 1}` {
			t.Errorf("MarshalJSON() = %s, want %s", string(data), `{"a": 1, "b": 1}`)
		}

		// Also check standard json.Marshal
		b, err := json.Marshal(sm)
		if err != nil {
			t.Fatalf("json.Marshal(sm) unexpected error: %v", err)
		}
		if len(b) == 0 {
			t.Errorf("json.Marshal(sm) produced empty output")
		}
	})

	t.Run("patternList MarshalJSON", func(t *testing.T) {
		pl := patternList{1, 2}
		data, err := pl.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON() unexpected error: %v", err)
		}
		if string(data) != `[1, 2]` {
			t.Errorf("MarshalJSON() = %s, want %s", string(data), `[1, 2]`)
		}
	})
}
