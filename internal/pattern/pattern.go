// Package pattern provides functions to normalize MongoDB query documents into
// canonical "shape" patterns where all leaf values are replaced with 1 and map keys
// are sorted alphabetically.
//
// In MongoDB log analysis, query patterns allow grouping similar queries together
// regardless of the specific literal filter or query values used.
package pattern

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// entry represents a single key-value pair within a sortedMap.
type entry struct {
	key   string
	value any
}

// sortedMap is an ordered slice of key-value pairs sorted alphabetically by key.
// It implements json.Marshaler to ensure deterministic key ordering and Python-compatible
// formatting (", " between entries and ": " between key and value).
type sortedMap []entry

// MarshalJSON marshals the sortedMap into JSON with Python-compatible separators (", ", ": ").
func (sm sortedMap) MarshalJSON() ([]byte, error) {
	return []byte(serializePattern(sm)), nil
}

// patternList represents a JSON array in a pattern.
// It implements json.Marshaler to format array elements with Python-compatible separators.
type patternList []any

// MarshalJSON marshals the patternList into JSON with Python-compatible separators.
func (pl patternList) MarshalJSON() ([]byte, error) {
	return []byte(serializePattern(pl)), nil
}

// marshalJSONNoEscape encodes a value into JSON bytes without escaping HTML characters.
func marshalJSONNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
	}
	return b, nil
}

// serializePattern converts a normalized pattern data structure into a string
// matching Python's json.dumps(sort_keys=True, separators=(", ", ": ")).
func serializePattern(v any) string {
	switch val := v.(type) {
	case sortedMap:
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, e := range val {
			if i > 0 {
				buf.WriteString(", ")
			}
			keyBytes, err := marshalJSONNoEscape(e.key)
			if err != nil {
				keyBytes, _ = json.Marshal(e.key)
			}
			buf.Write(keyBytes)
			buf.WriteString(": ")
			buf.WriteString(serializePattern(e.value))
		}
		buf.WriteByte('}')
		return buf.String()

	case patternList:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(serializePattern(item))
		}
		buf.WriteByte(']')
		return buf.String()

	default:
		b, err := marshalJSONNoEscape(val)
		if err != nil {
			b, _ = json.Marshal(val)
		}
		return string(b)
	}
}

// normalize recursively transforms a parsed JSON value into a pattern data structure.
// - Maps have their keys sorted alphabetically and values recursively normalized.
// - Non-empty slices keep only the first element (recursively normalized); empty slices remain empty.
// - All other types (strings, numbers, booleans, nil) are replaced with the integer 1.
func normalize(v any) any {
	if v == nil {
		return 1
	}

	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sm := make(sortedMap, 0, len(keys))
		for _, k := range keys {
			sm = append(sm, entry{
				key:   k,
				value: normalize(val[k]),
			})
		}
		return sm

	case []any:
		if len(val) == 0 {
			return patternList{}
		}
		return patternList{normalize(val[0])}

	case sortedMap:
		return val

	case patternList:
		return val

	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Map:
			mapKeys := rv.MapKeys()
			keys := make([]string, 0, len(mapKeys))
			keyMap := make(map[string]reflect.Value, len(mapKeys))
			for _, mk := range mapKeys {
				kStr := fmt.Sprint(mk.Interface())
				keys = append(keys, kStr)
				keyMap[kStr] = rv.MapIndex(mk)
			}
			sort.Strings(keys)
			sm := make(sortedMap, 0, len(keys))
			for _, k := range keys {
				sm = append(sm, entry{
					key:   k,
					value: normalize(keyMap[k].Interface()),
				})
			}
			return sm

		case reflect.Slice, reflect.Array:
			if rv.Len() == 0 {
				return patternList{}
			}
			return patternList{normalize(rv.Index(0).Interface())}

		default:
			return 1
		}
	}
}

// JSON2Pattern takes an already-parsed value (map, slice, or primitive from
// json.Unmarshal) and returns a canonical pattern string.
// It recursively replaces all leaf values with integer 1, keeps dict/list structure,
// sorts map keys alphabetically, and formats the output using separators matching
// Python's json.dumps(sort_keys=True, separators=(", ", ": ")).
//
// Example:
//
//	JSON2Pattern(map[string]interface{}{"b": "hello", "a": 42})
//	// Returns: `{"a": 1, "b": 1}`
func JSON2Pattern(v any) string {
	norm := normalize(v)
	return serializePattern(norm)
}

// JSON2PatternFromString parses a JSON string first, then calls JSON2Pattern.
// It is used for parsing pattern strings, such as from the --pattern CLI argument.
//
// Returns an error if the input string is not valid JSON.
func JSON2PatternFromString(s string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", err
	}
	return JSON2Pattern(v), nil
}
