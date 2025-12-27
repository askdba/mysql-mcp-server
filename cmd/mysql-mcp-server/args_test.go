package main

import (
	"testing"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantAction     string
		wantConfigPath string
		wantValidPath  string
		wantErr        bool
		errContains    string
	}{
		// Basic flags
		{
			name:       "no args",
			args:       []string{},
			wantAction: "",
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantAction: "version",
		},
		{
			name:       "version short flag",
			args:       []string{"-v"},
			wantAction: "version",
		},
		{
			name:       "help flag",
			args:       []string{"--help"},
			wantAction: "help",
		},
		{
			name:       "help short flag",
			args:       []string{"-h"},
			wantAction: "help",
		},
		{
			name:       "help command",
			args:       []string{"help"},
			wantAction: "help",
		},

		// Config flag
		{
			name:           "config with path",
			args:           []string{"--config", "/path/to/config.yaml"},
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:           "config short flag",
			args:           []string{"-c", "/path/to/config.yaml"},
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:           "config equals format",
			args:           []string{"--config=/path/to/config.yaml"},
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:        "config missing path",
			args:        []string{"--config"},
			wantErr:     true,
			errContains: "--config requires a path argument",
		},

		// Print config flag
		{
			name:       "print-config alone",
			args:       []string{"--print-config"},
			wantAction: "print-config",
		},

		// Validate config flag
		{
			name:          "validate-config with path",
			args:          []string{"--validate-config", "/path/to/config.yaml"},
			wantAction:    "validate-config",
			wantValidPath: "/path/to/config.yaml",
		},
		{
			name:        "validate-config missing path",
			args:        []string{"--validate-config"},
			wantErr:     true,
			errContains: "--validate-config requires a path argument",
		},

		// Combined flags - the key fix for this PR
		{
			name:           "config then print-config",
			args:           []string{"--config", "/path/to/config.yaml", "--print-config"},
			wantAction:     "print-config",
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:           "print-config then config (reverse order)",
			args:           []string{"--print-config", "--config", "/path/to/config.yaml"},
			wantAction:     "print-config",
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:           "config equals then print-config",
			args:           []string{"--config=/path/to/config.yaml", "--print-config"},
			wantAction:     "print-config",
			wantConfigPath: "/path/to/config.yaml",
		},
		{
			name:           "config then validate-config",
			args:           []string{"--config", "/base/config.yaml", "--validate-config", "/other/config.yaml"},
			wantAction:     "validate-config",
			wantConfigPath: "/base/config.yaml",
			wantValidPath:  "/other/config.yaml",
		},
		{
			name:           "short config then print-config",
			args:           []string{"-c", "/path/to/config.yaml", "--print-config"},
			wantAction:     "print-config",
			wantConfigPath: "/path/to/config.yaml",
		},

		// Error cases
		{
			name:        "unknown flag",
			args:        []string{"--unknown"},
			wantErr:     true,
			errContains: "unknown flag",
		},
		{
			name:        "unknown flag after config",
			args:        []string{"--config", "/path/to/config.yaml", "--badarg"},
			wantErr:     true,
			errContains: "unknown flag",
		},

		// Early exit flags (version/help) should return immediately
		{
			name:       "version stops processing",
			args:       []string{"--version", "--print-config"},
			wantAction: "version", // --print-config is not processed
		},
		{
			name:       "help stops processing",
			args:       []string{"--help", "--config", "/path"},
			wantAction: "help", // --config is not processed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseArgs(tt.args)

			// Check error
			if tt.wantErr {
				if result.err == nil {
					t.Errorf("parseArgs() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !contains(result.err.Error(), tt.errContains) {
					t.Errorf("parseArgs() error = %q, want error containing %q", result.err.Error(), tt.errContains)
				}
				return
			}

			if result.err != nil {
				t.Errorf("parseArgs() unexpected error: %v", result.err)
				return
			}

			// Check action
			if result.action != tt.wantAction {
				t.Errorf("parseArgs() action = %q, want %q", result.action, tt.wantAction)
			}

			// Check config path
			if result.configPath != tt.wantConfigPath {
				t.Errorf("parseArgs() configPath = %q, want %q", result.configPath, tt.wantConfigPath)
			}

			// Check validate path
			if result.validatePath != tt.wantValidPath {
				t.Errorf("parseArgs() validatePath = %q, want %q", result.validatePath, tt.wantValidPath)
			}
		})
	}
}

// contains checks if s contains substr (simple helper to avoid importing strings).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

