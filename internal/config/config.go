package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds selfhost_s3 server configuration
type Config struct {
	Bucket            string
	AccessKey         string
	SecretKey         string
	Port              int
	StoragePath       string
	Region            string
	CORSOrigins       []string
	MaxFileSize       int64  // in bytes
	PublicPrefix      string // prefix for publicly accessible files (default: "public/")
	PublicCacheMaxAge int    // Cache-Control max-age in seconds (default: 31536000)
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Port:              9000,
		StoragePath:       "./data",
		Region:            "us-east-1",
		CORSOrigins:       []string{"*"},
		MaxFileSize:       100 * 1024 * 1024, // 100MB default
		PublicPrefix:      "public/",         // default public prefix
		PublicCacheMaxAge: 31536000,          // 1 year default
	}

	// Required fields
	cfg.Bucket = os.Getenv("S3_BUCKET")
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required")
	}

	cfg.AccessKey = os.Getenv("S3_ACCESS_KEY")
	if cfg.AccessKey == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY is required")
	}

	cfg.SecretKey = os.Getenv("S3_SECRET_KEY")
	if cfg.SecretKey == "" {
		return nil, fmt.Errorf("S3_SECRET_KEY is required")
	}

	// Optional fields
	if port := os.Getenv("S3_PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("invalid S3_PORT: %w", err)
		}
		cfg.Port = p
	}

	if storagePath := os.Getenv("S3_STORAGE_PATH"); storagePath != "" {
		cfg.StoragePath = storagePath
	}

	if region := os.Getenv("S3_REGION"); region != "" {
		cfg.Region = region
	}

	if corsOrigins := os.Getenv("S3_CORS_ORIGINS"); corsOrigins != "" {
		cfg.CORSOrigins = strings.Split(corsOrigins, ",")
		for i, origin := range cfg.CORSOrigins {
			cfg.CORSOrigins[i] = strings.TrimSpace(origin)
		}
	}

	if maxSize := os.Getenv("S3_MAX_FILE_SIZE"); maxSize != "" {
		size, err := parseSize(maxSize)
		if err != nil {
			return nil, fmt.Errorf("invalid S3_MAX_FILE_SIZE: %w", err)
		}
		cfg.MaxFileSize = size
	}

	// Public prefix configuration
	if publicPrefix, exists := os.LookupEnv("S3_PUBLIC_PREFIX"); exists {
		if publicPrefix == "" {
			cfg.PublicPrefix = ""
		} else {
			if !strings.HasSuffix(publicPrefix, "/") {
				publicPrefix = publicPrefix + "/"
			}
			cfg.PublicPrefix = publicPrefix
		}
	}

	// Public cache max age configuration
	if maxAge := os.Getenv("S3_PUBLIC_CACHE_MAX_AGE"); maxAge != "" {
		if age, err := strconv.Atoi(maxAge); err == nil {
			cfg.PublicCacheMaxAge = age
		}
	}

	return cfg, nil
}

// parseSize parses a size string like "100MB" into bytes
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))

	multiplier := int64(1)
	if strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, err
	}

	return n * multiplier, nil
}
