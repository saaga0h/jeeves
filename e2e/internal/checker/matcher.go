package checker

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// MatchesExpectation checks if actual value matches expected value
// Returns (true, "") on match, (false, "reason") on mismatch
func MatchesExpectation(actual, expected interface{}) (bool, string) {
	// Handle nil cases
	if expected == nil && actual == nil {
		return true, ""
	}
	if expected == nil && actual != nil {
		return false, fmt.Sprintf("expected nil, got %v", actual)
	}
	if expected != nil && actual == nil {
		return false, fmt.Sprintf("expected %v, got nil", expected)
	}

	// Get the types
	actualType := reflect.TypeOf(actual)
	expectedType := reflect.TypeOf(expected)

	// Special case: expected is string - check for matchers
	if expectedType.Kind() == reflect.String {
		expectedStr := expected.(string)

		// Check for regex matcher: ~pattern~
		if strings.HasPrefix(expectedStr, "~") && strings.HasSuffix(expectedStr, "~") {
			pattern := strings.Trim(expectedStr, "~")
			return matchRegex(actual, pattern)
		}

		// Check for comparison matchers: >value, <value, >=value, <=value
		if strings.HasPrefix(expectedStr, ">") || strings.HasPrefix(expectedStr, "<") {
			return matchComparison(actual, expectedStr)
		}
	}

	// Type mismatch check (but allow numeric conversions)
	if !typesCompatible(actualType, expectedType) {
		return false, fmt.Sprintf("type mismatch: expected %s, got %s", expectedType, actualType)
	}

	// Handle different types
	switch expectedType.Kind() {
	case reflect.String:
		return matchString(actual, expected.(string))

	case reflect.Bool:
		return matchBool(actual, expected.(bool))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return matchNumber(actual, expected)

	case reflect.Float32, reflect.Float64:
		return matchNumber(actual, expected)

	case reflect.Map:
		return matchMap(actual, expected)

	case reflect.Slice, reflect.Array:
		return matchArray(actual, expected)

	default:
		// Direct comparison
		if reflect.DeepEqual(actual, expected) {
			return true, ""
		}
		return false, fmt.Sprintf("expected %v, got %v", expected, actual)
	}
}

// matchString performs string matching
func matchString(actual interface{}, expected string) (bool, string) {
	actualStr, ok := actual.(string)
	if !ok {
		return false, fmt.Sprintf("expected string, got %T", actual)
	}

	if actualStr == expected {
		return true, ""
	}

	return false, fmt.Sprintf("expected %q, got %q", expected, actualStr)
}

// matchBool performs boolean matching
func matchBool(actual interface{}, expected bool) (bool, string) {
	actualBool, ok := actual.(bool)
	if !ok {
		return false, fmt.Sprintf("expected bool, got %T", actual)
	}

	if actualBool == expected {
		return true, ""
	}

	return false, fmt.Sprintf("expected %v, got %v", expected, actualBool)
}

// matchNumber performs numeric matching with type conversion
func matchNumber(actual, expected interface{}) (bool, string) {
	actualFloat, err := toFloat64(actual)
	if err != nil {
		return false, fmt.Sprintf("actual value is not numeric: %v", actual)
	}

	expectedFloat, err := toFloat64(expected)
	if err != nil {
		return false, fmt.Sprintf("expected value is not numeric: %v", expected)
	}

	if actualFloat == expectedFloat {
		return true, ""
	}

	return false, fmt.Sprintf("expected %v, got %v", expected, actual)
}

// matchRegex checks if actual matches a regex pattern
func matchRegex(actual interface{}, pattern string) (bool, string) {
	// Convert actual to string
	actualStr := fmt.Sprintf("%v", actual)

	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Sprintf("invalid regex pattern %q: %v", pattern, err)
	}

	if re.MatchString(actualStr) {
		return true, ""
	}

	return false, fmt.Sprintf("value %q does not match pattern ~%s~", actualStr, pattern)
}

// matchComparison checks if actual satisfies a comparison (>, <, >=, <=)
func matchComparison(actual interface{}, comparison string) (bool, string) {
	actualFloat, err := toFloat64(actual)
	if err != nil {
		return false, fmt.Sprintf("cannot compare non-numeric value: %v", actual)
	}

	// Parse comparison
	var op string
	var valueStr string

	if strings.HasPrefix(comparison, ">=") {
		op = ">="
		valueStr = strings.TrimPrefix(comparison, ">=")
	} else if strings.HasPrefix(comparison, "<=") {
		op = "<="
		valueStr = strings.TrimPrefix(comparison, "<=")
	} else if strings.HasPrefix(comparison, ">") {
		op = ">"
		valueStr = strings.TrimPrefix(comparison, ">")
	} else if strings.HasPrefix(comparison, "<") {
		op = "<"
		valueStr = strings.TrimPrefix(comparison, "<")
	} else {
		return false, fmt.Sprintf("invalid comparison: %s", comparison)
	}

	expectedFloat, err := strconv.ParseFloat(strings.TrimSpace(valueStr), 64)
	if err != nil {
		return false, fmt.Sprintf("invalid comparison value: %s", valueStr)
	}

	var result bool
	switch op {
	case ">":
		result = actualFloat > expectedFloat
	case "<":
		result = actualFloat < expectedFloat
	case ">=":
		result = actualFloat >= expectedFloat
	case "<=":
		result = actualFloat <= expectedFloat
	}

	if result {
		return true, ""
	}

	return false, fmt.Sprintf("expected %v %s %v, but got %v", actual, op, expectedFloat, actualFloat)
}

// matchMap performs recursive matching on maps
func matchMap(actual, expected interface{}) (bool, string) {
	actualMap, ok := actual.(map[string]interface{})
	if !ok {
		return false, fmt.Sprintf("expected map, got %T", actual)
	}

	expectedMap, ok := expected.(map[string]interface{})
	if !ok {
		return false, fmt.Sprintf("expected value is not a map: %T", expected)
	}

	// Check all expected keys
	for key, expectedValue := range expectedMap {
		actualValue, exists := actualMap[key]
		if !exists {
			return false, fmt.Sprintf("missing key %q", key)
		}

		matches, reason := MatchesExpectation(actualValue, expectedValue)
		if !matches {
			return false, fmt.Sprintf("key %q: %s", key, reason)
		}
	}

	return true, ""
}

// matchArray performs element-wise matching on arrays
func matchArray(actual, expected interface{}) (bool, string) {
	actualVal := reflect.ValueOf(actual)
	expectedVal := reflect.ValueOf(expected)

	if actualVal.Len() != expectedVal.Len() {
		return false, fmt.Sprintf("expected array length %d, got %d", expectedVal.Len(), actualVal.Len())
	}

	for i := 0; i < expectedVal.Len(); i++ {
		actualElem := actualVal.Index(i).Interface()
		expectedElem := expectedVal.Index(i).Interface()

		matches, reason := MatchesExpectation(actualElem, expectedElem)
		if !matches {
			return false, fmt.Sprintf("element %d: %s", i, reason)
		}
	}

	return true, ""
}

// toFloat64 converts various numeric types to float64
func toFloat64(val interface{}) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("not a numeric type: %T", val)
	}
}

// typesCompatible checks if two types are compatible for comparison
func typesCompatible(t1, t2 reflect.Type) bool {
	// Same type
	if t1 == t2 {
		return true
	}

	// Both numeric
	if isNumeric(t1) && isNumeric(t2) {
		return true
	}

	// Both strings
	if t1.Kind() == reflect.String && t2.Kind() == reflect.String {
		return true
	}

	// Both bools
	if t1.Kind() == reflect.Bool && t2.Kind() == reflect.Bool {
		return true
	}

	// Both maps
	if t1.Kind() == reflect.Map && t2.Kind() == reflect.Map {
		return true
	}

	// Both slices/arrays
	if (t1.Kind() == reflect.Slice || t1.Kind() == reflect.Array) &&
		(t2.Kind() == reflect.Slice || t2.Kind() == reflect.Array) {
		return true
	}

	return false
}

// isNumeric checks if a type is numeric
func isNumeric(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
