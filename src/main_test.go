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

func TestParseProxyProtocolOption(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedEnabled bool
		expectedVersion byte
		expectError     bool
	}{
		{
			name:            "empty string - disabled",
			input:           "",
			expectedEnabled: false,
			expectedVersion: 0,
			expectError:     false,
		},
		{
			name:            "v1 string",
			input:           "v1",
			expectedEnabled: true,
			expectedVersion: 1,
			expectError:     false,
		},
		{
			name:            "v2 string",
			input:           "v2",
			expectedEnabled: true,
			expectedVersion: 2,
			expectError:     false,
		},
		{
			name:            "numeric 1",
			input:           "1",
			expectedEnabled: true,
			expectedVersion: 1,
			expectError:     false,
		},
		{
			name:            "numeric 2",
			input:           "2",
			expectedEnabled: true,
			expectedVersion: 2,
			expectError:     false,
		},
		{
			name:            "invalid version v3",
			input:           "v3",
			expectedEnabled: false,
			expectedVersion: 0,
			expectError:     true,
		},
		{
			name:            "invalid version 3",
			input:           "3",
			expectedEnabled: false,
			expectedVersion: 0,
			expectError:     true,
		},
		{
			name:            "invalid string",
			input:           "invalid",
			expectedEnabled: false,
			expectedVersion: 0,
			expectError:     true,
		},
		{
			name:            "uppercase V1",
			input:           "V1",
			expectedEnabled: false,
			expectedVersion: 0,
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, version, err := parseProxyProtocolOption(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none for input %q", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for input %q: %v", tt.input, err)
				}
				if enabled != tt.expectedEnabled {
					t.Errorf("for input %q: expected enabled=%v, got %v", tt.input, tt.expectedEnabled, enabled)
				}
				if version != tt.expectedVersion {
					t.Errorf("for input %q: expected version=%d, got %d", tt.input, tt.expectedVersion, version)
				}
			}
		})
	}
}

func TestParseProxyProtocolConfig(t *testing.T) {
	tests := []struct {
		name           string
		options        []string
		globalClient   bool
		globalServer   bool
		expectedConfig ProxyProtocolConfig
		expectError    bool
	}{
		{
			name:         "no options, no globals",
			options:      []string{},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: false,
				ClientVersion: 0,
			},
			expectError: false,
		},
		{
			name:         "server v1 per-mapping",
			options:      []string{"proxy-server=v1"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 1,
				ClientEnabled: false,
				ClientVersion: 0,
			},
			expectError: false,
		},
		{
			name:         "client v2 per-mapping",
			options:      []string{"proxy-client=v2"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: true,
				ClientVersion: 2,
			},
			expectError: false,
		},
		{
			name:         "both v1 per-mapping",
			options:      []string{"proxy-server=v1", "proxy-client=v1"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 1,
				ClientEnabled: true,
				ClientVersion: 1,
			},
			expectError: false,
		},
		{
			name:         "mixed versions per-mapping",
			options:      []string{"proxy-server=v2", "proxy-client=v1"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 2,
				ClientEnabled: true,
				ClientVersion: 1,
			},
			expectError: false,
		},
		{
			name:         "global server only",
			options:      []string{},
			globalClient: false,
			globalServer: true,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 1, // defaults to v1
				ClientEnabled: false,
				ClientVersion: 0,
			},
			expectError: false,
		},
		{
			name:         "global client only",
			options:      []string{},
			globalClient: true,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: true,
				ClientVersion: 1, // defaults to v1
			},
			expectError: false,
		},
		{
			name:         "both globals enabled",
			options:      []string{},
			globalClient: true,
			globalServer: true,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 1,
				ClientEnabled: true,
				ClientVersion: 1,
			},
			expectError: false,
		},
		{
			name:         "per-mapping overrides global",
			options:      []string{"proxy-server=v2"},
			globalClient: true,
			globalServer: true,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 2, // per-mapping v2 overrides global v1
				ClientEnabled: true,
				ClientVersion: 1, // falls back to global
			},
			expectError: false,
		},
		{
			name:         "with other options",
			options:      []string{"http", "proxy-client=v2", "lb=random"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: true,
				ClientVersion: 2,
			},
			expectError: false,
		},
		{
			name:         "invalid server version",
			options:      []string{"proxy-server=v3"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: false,
				ClientVersion: 0,
			},
			expectError: true,
		},
		{
			name:         "invalid client version",
			options:      []string{"proxy-client=invalid"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: false,
				ServerVersion: 0,
				ClientEnabled: false,
				ClientVersion: 0,
			},
			expectError: true,
		},
		{
			name:         "numeric versions",
			options:      []string{"proxy-server=1", "proxy-client=2"},
			globalClient: false,
			globalServer: false,
			expectedConfig: ProxyProtocolConfig{
				ServerEnabled: true,
				ServerVersion: 1,
				ClientEnabled: true,
				ClientVersion: 2,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := parseProxyProtocolConfig(tt.options, tt.globalClient, tt.globalServer)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if config.ServerEnabled != tt.expectedConfig.ServerEnabled {
					t.Errorf("expected ServerEnabled=%v, got %v", tt.expectedConfig.ServerEnabled, config.ServerEnabled)
				}
				if config.ServerVersion != tt.expectedConfig.ServerVersion {
					t.Errorf("expected ServerVersion=%d, got %d", tt.expectedConfig.ServerVersion, config.ServerVersion)
				}
				if config.ClientEnabled != tt.expectedConfig.ClientEnabled {
					t.Errorf("expected ClientEnabled=%v, got %v", tt.expectedConfig.ClientEnabled, config.ClientEnabled)
				}
				if config.ClientVersion != tt.expectedConfig.ClientVersion {
					t.Errorf("expected ClientVersion=%d, got %d", tt.expectedConfig.ClientVersion, config.ClientVersion)
				}
			}
		})
	}
}
