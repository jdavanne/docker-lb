package main

import (
	"fmt"
	"reflect"
	"testing"
)

// expandBackendPorts expands a single backend port to match listen port range length
// This mirrors the logic in smain() for testing purposes
func expandBackendPorts(listenPorts, backendPorts []string) ([]string, error) {
	if len(listenPorts) != len(backendPorts) {
		if len(backendPorts) == 1 {
			singlePort := backendPorts[0]
			expanded := make([]string, len(listenPorts))
			for j := range expanded {
				expanded[j] = singlePort
			}
			return expanded, nil
		}
		return nil, fmt.Errorf("port range mismatch: %d listen ports, %d backend ports", len(listenPorts), len(backendPorts))
	}
	return backendPorts, nil
}

func TestExpandBackendPorts(t *testing.T) {
	tests := []struct {
		name        string
		listenPorts []string
		backendPorts []string
		expected    []string
		expectError bool
	}{
		{
			name:        "same length ranges",
			listenPorts: []string{"8080", "8081", "8082"},
			backendPorts: []string{"9000", "9001", "9002"},
			expected:    []string{"9000", "9001", "9002"},
			expectError: false,
		},
		{
			name:        "single backend port expanded",
			listenPorts: []string{"30000", "30001", "30002"},
			backendPorts: []string{"9090"},
			expected:    []string{"9090", "9090", "9090"},
			expectError: false,
		},
		{
			name:        "large range to single port",
			listenPorts: []string{"30000", "30001", "30002", "30003", "30004"},
			backendPorts: []string{"80"},
			expected:    []string{"80", "80", "80", "80", "80"},
			expectError: false,
		},
		{
			name:        "single to single",
			listenPorts: []string{"8080"},
			backendPorts: []string{"9090"},
			expected:    []string{"9090"},
			expectError: false,
		},
		{
			name:        "mismatched ranges (not single)",
			listenPorts: []string{"8080", "8081", "8082"},
			backendPorts: []string{"9000", "9001"},
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := expandBackendPorts(tt.listenPorts, tt.backendPorts)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				} else if !reflect.DeepEqual(result, tt.expected) {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			}
		})
	}
}

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

func TestParseTargets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []backendTarget
		wantErr  bool
	}{
		{
			name:     "single host:port",
			input:    "service1:9000",
			expected: []backendTarget{{host: "service1", port: "9000"}},
		},
		{
			name:     "single host:port-range",
			input:    "service1:9000-9003",
			expected: []backendTarget{{host: "service1", port: "9000-9003"}},
		},
		{
			name:  "same host multiple ports",
			input: "service1:9000+9001+9002",
			expected: []backendTarget{
				{host: "service1", port: "9000"},
				{host: "service1", port: "9001"},
				{host: "service1", port: "9002"},
			},
		},
		{
			name:  "multiple host:port",
			input: "service1:9000+service2:9001",
			expected: []backendTarget{
				{host: "service1", port: "9000"},
				{host: "service2", port: "9001"},
			},
		},
		{
			name:  "multiple host:port with port-only",
			input: "service1:9000+9001+service2:8080",
			expected: []backendTarget{
				{host: "service1", port: "9000"},
				{host: "service1", port: "9001"},
				{host: "service2", port: "8080"},
			},
		},
		{
			name:    "port-only without preceding host",
			input:   "9000",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTargets(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
