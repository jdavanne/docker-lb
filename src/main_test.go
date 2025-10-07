package main

import (
	"reflect"
	"testing"
)

func TestParsePortRange(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string
		expectError bool
	}{
		{
			name:        "single port",
			input:       "8080",
			expected:    []string{"8080"},
			expectError: false,
		},
		{
			name:        "port range with 4 ports",
			input:       "8080-8083",
			expected:    []string{"8080", "8081", "8082", "8083"},
			expectError: false,
		},
		{
			name:        "port range with 1 port (equal values)",
			input:       "8090-8090",
			expected:    []string{"8090"},
			expectError: false,
		},
		{
			name:        "large port range",
			input:       "9000-9010",
			expected:    []string{"9000", "9001", "9002", "9003", "9004", "9005", "9006", "9007", "9008", "9009", "9010"},
			expectError: false,
		},
		{
			name:        "low port numbers",
			input:       "80-82",
			expected:    []string{"80", "81", "82"},
			expectError: false,
		},
		{
			name:        "high port numbers",
			input:       "65533-65535",
			expected:    []string{"65533", "65534", "65535"},
			expectError: false,
		},
		{
			name:        "invalid range - port1 > port2",
			input:       "8090-8089",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid range - non-numeric start",
			input:       "abc-8090",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid range - non-numeric end",
			input:       "8080-xyz",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid range - both non-numeric",
			input:       "abc-def",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - multiple dashes",
			input:       "8080-8085-8090",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - empty start",
			input:       "-8090",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - empty end",
			input:       "8080-",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid format - just dash",
			input:       "-",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "non-numeric single port",
			input:       "http",
			expected:    []string{"http"},
			expectError: false,
		},
		{
			name:        "port with leading zeros",
			input:       "08080",
			expected:    []string{"08080"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parsePortRange(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none for input %q", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				} else if !reflect.DeepEqual(result, tt.expected) {
					t.Errorf("for input %q: expected %v, got %v", tt.input, tt.expected, result)
				}
			}
		})
	}
}
