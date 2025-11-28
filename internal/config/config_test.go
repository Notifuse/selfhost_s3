package config

import (
	"os"
	"testing"
)

func TestLoad_RequiredFields(t *testing.T) {
	// Clear all env vars first
	clearEnvVars()

	tests := []struct {
		name        string
		envVars     map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing all required fields",
			envVars:     map[string]string{},
			expectError: true,
			errorMsg:    "S3_BUCKET is required",
		},
		{
			name: "missing access key",
			envVars: map[string]string{
				"S3_BUCKET": "test-bucket",
			},
			expectError: true,
			errorMsg:    "S3_ACCESS_KEY is required",
		},
		{
			name: "missing secret key",
			envVars: map[string]string{
				"S3_BUCKET":     "test-bucket",
				"S3_ACCESS_KEY": "access-key",
			},
			expectError: true,
			errorMsg:    "S3_SECRET_KEY is required",
		},
		{
			name: "all required fields present",
			envVars: map[string]string{
				"S3_BUCKET":     "test-bucket",
				"S3_ACCESS_KEY": "access-key",
				"S3_SECRET_KEY": "secret-key",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}

			cfg, err := Load()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if cfg == nil {
					t.Error("expected config, got nil")
				}
			}
		})
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	clearEnvVars()
	_ = os.Setenv("S3_BUCKET", "test-bucket")
	_ = os.Setenv("S3_ACCESS_KEY", "access-key")
	_ = os.Setenv("S3_SECRET_KEY", "secret-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify defaults
	if cfg.Port != 9000 {
		t.Errorf("expected default port 9000, got %d", cfg.Port)
	}
	if cfg.StoragePath != "./data" {
		t.Errorf("expected default storage path './data', got %q", cfg.StoragePath)
	}
	if cfg.Region != "us-east-1" {
		t.Errorf("expected default region 'us-east-1', got %q", cfg.Region)
	}
	if len(cfg.CORSOrigins) != 1 || cfg.CORSOrigins[0] != "*" {
		t.Errorf("expected default CORS origins ['*'], got %v", cfg.CORSOrigins)
	}
	if cfg.MaxFileSize != 100*1024*1024 {
		t.Errorf("expected default max file size 100MB, got %d", cfg.MaxFileSize)
	}
}

func TestLoad_OptionalFields(t *testing.T) {
	clearEnvVars()
	_ = os.Setenv("S3_BUCKET", "my-bucket")
	_ = os.Setenv("S3_ACCESS_KEY", "my-access-key")
	_ = os.Setenv("S3_SECRET_KEY", "my-secret-key")
	_ = os.Setenv("S3_PORT", "8080")
	_ = os.Setenv("S3_STORAGE_PATH", "/var/data/s3")
	_ = os.Setenv("S3_REGION", "eu-west-1")
	_ = os.Setenv("S3_CORS_ORIGINS", "https://example.com, https://app.example.com")
	_ = os.Setenv("S3_MAX_FILE_SIZE", "50MB")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Bucket != "my-bucket" {
		t.Errorf("expected bucket 'my-bucket', got %q", cfg.Bucket)
	}
	if cfg.AccessKey != "my-access-key" {
		t.Errorf("expected access key 'my-access-key', got %q", cfg.AccessKey)
	}
	if cfg.SecretKey != "my-secret-key" {
		t.Errorf("expected secret key 'my-secret-key', got %q", cfg.SecretKey)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.StoragePath != "/var/data/s3" {
		t.Errorf("expected storage path '/var/data/s3', got %q", cfg.StoragePath)
	}
	if cfg.Region != "eu-west-1" {
		t.Errorf("expected region 'eu-west-1', got %q", cfg.Region)
	}
	if len(cfg.CORSOrigins) != 2 {
		t.Errorf("expected 2 CORS origins, got %d", len(cfg.CORSOrigins))
	}
	if cfg.CORSOrigins[0] != "https://example.com" {
		t.Errorf("expected first CORS origin 'https://example.com', got %q", cfg.CORSOrigins[0])
	}
	if cfg.CORSOrigins[1] != "https://app.example.com" {
		t.Errorf("expected second CORS origin 'https://app.example.com', got %q", cfg.CORSOrigins[1])
	}
	if cfg.MaxFileSize != 50*1024*1024 {
		t.Errorf("expected max file size 50MB (52428800), got %d", cfg.MaxFileSize)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	clearEnvVars()
	_ = os.Setenv("S3_BUCKET", "test-bucket")
	_ = os.Setenv("S3_ACCESS_KEY", "access-key")
	_ = os.Setenv("S3_SECRET_KEY", "secret-key")
	_ = os.Setenv("S3_PORT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid port, got nil")
	}
}

func TestLoad_InvalidMaxFileSize(t *testing.T) {
	clearEnvVars()
	_ = os.Setenv("S3_BUCKET", "test-bucket")
	_ = os.Setenv("S3_ACCESS_KEY", "access-key")
	_ = os.Setenv("S3_SECRET_KEY", "secret-key")
	_ = os.Setenv("S3_MAX_FILE_SIZE", "invalid")

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid max file size, got nil")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		hasError bool
	}{
		{"100", 100, false},
		{"100B", 100, false},
		{"100b", 100, false},
		{"10KB", 10 * 1024, false},
		{"10kb", 10 * 1024, false},
		{"10MB", 10 * 1024 * 1024, false},
		{"10mb", 10 * 1024 * 1024, false},
		{"2GB", 2 * 1024 * 1024 * 1024, false},
		{"2gb", 2 * 1024 * 1024 * 1024, false},
		{"  50MB  ", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSize(tt.input)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
				return
			}

			if result != tt.expected {
				t.Errorf("parseSize(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoad_PublicPrefix(t *testing.T) {
	tests := []struct {
		name           string
		envValue       string
		setEnv         bool
		expectedPrefix string
	}{
		{
			name:           "default value when not set",
			setEnv:         false,
			expectedPrefix: "public/",
		},
		{
			name:           "custom prefix without trailing slash",
			envValue:       "assets",
			setEnv:         true,
			expectedPrefix: "assets/",
		},
		{
			name:           "custom prefix with trailing slash",
			envValue:       "files/",
			setEnv:         true,
			expectedPrefix: "files/",
		},
		{
			name:           "empty string disables public access",
			envValue:       "",
			setEnv:         true,
			expectedPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			_ = os.Setenv("S3_BUCKET", "test-bucket")
			_ = os.Setenv("S3_ACCESS_KEY", "access-key")
			_ = os.Setenv("S3_SECRET_KEY", "secret-key")

			if tt.setEnv {
				_ = os.Setenv("S3_PUBLIC_PREFIX", tt.envValue)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.PublicPrefix != tt.expectedPrefix {
				t.Errorf("expected PublicPrefix %q, got %q", tt.expectedPrefix, cfg.PublicPrefix)
			}
		})
	}
}

func TestLoad_PublicCacheMaxAge(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		setEnv      bool
		expectedAge int
	}{
		{
			name:        "default value when not set",
			setEnv:      false,
			expectedAge: 31536000, // 1 year
		},
		{
			name:        "custom value",
			envValue:    "3600",
			setEnv:      true,
			expectedAge: 3600,
		},
		{
			name:        "zero disables caching",
			envValue:    "0",
			setEnv:      true,
			expectedAge: 0,
		},
		{
			name:        "invalid value uses default",
			envValue:    "invalid",
			setEnv:      true,
			expectedAge: 31536000, // keeps default on invalid
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			_ = os.Setenv("S3_BUCKET", "test-bucket")
			_ = os.Setenv("S3_ACCESS_KEY", "access-key")
			_ = os.Setenv("S3_SECRET_KEY", "secret-key")

			if tt.setEnv {
				_ = os.Setenv("S3_PUBLIC_CACHE_MAX_AGE", tt.envValue)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.PublicCacheMaxAge != tt.expectedAge {
				t.Errorf("expected PublicCacheMaxAge %d, got %d", tt.expectedAge, cfg.PublicCacheMaxAge)
			}
		})
	}
}

func clearEnvVars() {
	envVars := []string{
		"S3_BUCKET",
		"S3_ACCESS_KEY",
		"S3_SECRET_KEY",
		"S3_PORT",
		"S3_STORAGE_PATH",
		"S3_REGION",
		"S3_CORS_ORIGINS",
		"S3_MAX_FILE_SIZE",
		"S3_PUBLIC_PREFIX",
		"S3_PUBLIC_CACHE_MAX_AGE",
	}
	for _, v := range envVars {
		_ = os.Unsetenv(v)
	}
}
