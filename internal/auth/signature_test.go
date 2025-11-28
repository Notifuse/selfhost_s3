package auth

import (
	"crypto/hmac"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewSignatureV4(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")
	if sig == nil {
		t.Fatal("expected SignatureV4 instance, got nil")
	}

	if sig.creds.AccessKey != "access-key" {
		t.Errorf("expected access key 'access-key', got %q", sig.creds.AccessKey)
	}
	if sig.creds.SecretKey != "secret-key" {
		t.Errorf("expected secret key 'secret-key', got %q", sig.creds.SecretKey)
	}
	if sig.creds.Region != "us-east-1" {
		t.Errorf("expected region 'us-east-1', got %q", sig.creds.Region)
	}
}

func TestValidateRequest_MissingAuthHeader(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for missing Authorization header")
	}
	if !strings.Contains(err.Error(), "Authorization header") {
		t.Errorf("expected error about Authorization header, got %v", err)
	}
}

func TestValidateRequest_InvalidAuthHeader(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for invalid Authorization header format")
	}
}

func TestValidateRequest_MissingAmzDate(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=access-key/20231215/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123")

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for missing X-Amz-Date header")
	}
	if !strings.Contains(err.Error(), "X-Amz-Date") {
		t.Errorf("expected error about X-Amz-Date, got %v", err)
	}
}

func TestValidateRequest_WrongAccessKey(t *testing.T) {
	sig := NewSignatureV4("correct-key", "secret-key", "us-east-1")

	amzDate := time.Now().UTC().Format("20060102T150405Z")

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=wrong-key/20231215/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123")
	req.Header.Set("X-Amz-Date", amzDate)

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for wrong access key")
	}
	if !strings.Contains(err.Error(), "invalid access key") {
		t.Errorf("expected error about invalid access key, got %v", err)
	}
}

func TestValidateRequest_ExpiredRequest(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")

	// Use a date from 1 hour ago
	oldDate := time.Now().UTC().Add(-1 * time.Hour).Format("20060102T150405Z")

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=access-key/20231215/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123")
	req.Header.Set("X-Amz-Date", oldDate)

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for expired request")
	}
	if !strings.Contains(err.Error(), "timestamp") {
		t.Errorf("expected error about timestamp, got %v", err)
	}
}

func TestValidateRequest_ValidSignature(t *testing.T) {
	accessKey := "AKIAIOSFODNN7EXAMPLE"
	secretKey := "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	region := "us-east-1"

	sig := NewSignatureV4(accessKey, secretKey, region)

	// Create a request
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	req := httptest.NewRequest(http.MethodGet, "http://localhost:9000/test-bucket/test-key", nil)
	req.Host = "localhost:9000"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")

	// Calculate signature manually
	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}

	// Create canonical request
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-content-sha256:UNSIGNED-PAYLOAD\nx-amz-date:%s\n",
		req.Host, amzDate)
	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		"GET",
		"/test-bucket/test-key",
		"",
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		"UNSIGNED-PAYLOAD",
	)

	// Create string to sign
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	hashedCanonical := hashSHA256(canonicalRequest)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		scope,
		hashedCanonical,
	)

	// Derive signing key
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")

	// Calculate signature
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	// Set Authorization header
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s",
		accessKey, dateStamp, region, strings.Join(signedHeaders, ";"), signature)
	req.Header.Set("Authorization", authHeader)

	err := sig.ValidateRequest(req)
	if err != nil {
		t.Errorf("expected valid signature to pass, got error: %v", err)
	}
}

func TestValidateRequest_InvalidSignature(t *testing.T) {
	sig := NewSignatureV4("access-key", "secret-key", "us-east-1")

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	req := httptest.NewRequest(http.MethodGet, "http://localhost:9000/test-bucket/key", nil)
	req.Host = "localhost:9000"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")

	// Set Authorization with wrong signature
	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=access-key/%s/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=0000000000000000000000000000000000000000000000000000000000000000",
		dateStamp)
	req.Header.Set("Authorization", authHeader)

	err := sig.ValidateRequest(req)
	if err == nil {
		t.Error("expected error for invalid signature")
	}
	if !strings.Contains(err.Error(), "signature mismatch") {
		t.Errorf("expected signature mismatch error, got %v", err)
	}
}

func TestParseAuthHeader(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		expectError bool
		accessKey   string
		date        string
		region      string
	}{
		{
			name:        "valid header",
			header:      "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20231215/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123def456",
			expectError: false,
			accessKey:   "AKIAIOSFODNN7EXAMPLE",
			date:        "20231215",
			region:      "us-east-1",
		},
		{
			name:        "invalid format",
			header:      "Basic dXNlcjpwYXNz",
			expectError: true,
		},
		{
			name:        "missing parts",
			header:      "AWS4-HMAC-SHA256 Credential=test",
			expectError: true,
		},
		{
			name:        "invalid credential format",
			header:      "AWS4-HMAC-SHA256 Credential=test/only/three, SignedHeaders=host, Signature=abc",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := parseAuthHeader(tt.header)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if auth.AccessKey != tt.accessKey {
				t.Errorf("expected access key %q, got %q", tt.accessKey, auth.AccessKey)
			}
			if auth.Date != tt.date {
				t.Errorf("expected date %q, got %q", tt.date, auth.Date)
			}
			if auth.Region != tt.region {
				t.Errorf("expected region %q, got %q", tt.region, auth.Region)
			}
		})
	}
}

func TestCreateCanonicalQueryString(t *testing.T) {
	sig := NewSignatureV4("key", "secret", "us-east-1")

	tests := []struct {
		name     string
		query    map[string][]string
		expected string
	}{
		{
			name:     "empty query",
			query:    map[string][]string{},
			expected: "",
		},
		{
			name:     "single param",
			query:    map[string][]string{"key": {"value"}},
			expected: "key=value",
		},
		{
			name:     "multiple params sorted",
			query:    map[string][]string{"b": {"2"}, "a": {"1"}, "c": {"3"}},
			expected: "a=1&b=2&c=3",
		},
		{
			name:     "params needing encoding",
			query:    map[string][]string{"key": {"hello world"}},
			expected: "key=hello+world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sig.createCanonicalQueryString(tt.query)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDeriveSigningKey(t *testing.T) {
	// Use AWS test vector values
	sig := NewSignatureV4("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", "us-east-1")

	signingKey := sig.deriveSigningKey("20150830")

	// The signing key should be deterministic
	if len(signingKey) != 32 { // SHA256 produces 32 bytes
		t.Errorf("expected 32 byte signing key, got %d bytes", len(signingKey))
	}

	// Same inputs should produce same key
	signingKey2 := sig.deriveSigningKey("20150830")
	if !hmac.Equal(signingKey, signingKey2) {
		t.Error("signing key should be deterministic")
	}

	// Different date should produce different key
	signingKey3 := sig.deriveSigningKey("20150831")
	if hmac.Equal(signingKey, signingKey3) {
		t.Error("different dates should produce different signing keys")
	}
}

func TestHashSHA256(t *testing.T) {
	// Test with known values
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			input:    "hello",
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := hashSHA256(tt.input)
			if result != tt.expected {
				t.Errorf("hashSHA256(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHmacSHA256(t *testing.T) {
	key := []byte("secret")
	data := "message"

	result := hmacSHA256(key, data)

	// HMAC-SHA256 produces 32 bytes
	if len(result) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(result))
	}

	// Should be deterministic
	result2 := hmacSHA256(key, data)
	if !hmac.Equal(result, result2) {
		t.Error("HMAC should be deterministic")
	}

	// Known test vector
	expectedHex := "8b5f48702995c1598c573db1e21866a9b825d4a794d169d7060a03605796360b"
	if hex.EncodeToString(result) != expectedHex {
		t.Errorf("expected %s, got %s", expectedHex, hex.EncodeToString(result))
	}
}

// Test helper to verify signature calculation matches AWS SDK behavior
func TestSignatureCalculation_RoundTrip(t *testing.T) {
	accessKey := "test-access-key"
	secretKey := "test-secret-key"
	region := "us-west-2"

	sig := NewSignatureV4(accessKey, secretKey, region)

	// Create a properly signed request
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	req := httptest.NewRequest(http.MethodPut, "http://s3.localhost:9000/mybucket/mykey", strings.NewReader("test content"))
	req.Host = "s3.localhost:9000"
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", hashSHA256("test content"))
	req.Header.Set("Content-Type", "text/plain")

	// Sign the request
	signedHeaders := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date"}

	canonicalHeaders := ""
	for _, h := range signedHeaders {
		var val string
		if h == "host" {
			val = req.Host
		} else {
			val = req.Header.Get(h)
		}
		canonicalHeaders += fmt.Sprintf("%s:%s\n", h, val)
	}

	payloadHash := req.Header.Get("X-Amz-Content-Sha256")

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		"PUT",
		"/mybucket/mykey",
		"",
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		payloadHash,
	)

	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		scope,
		hashSHA256(canonicalRequest),
	)

	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")

	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	authHeader := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s",
		accessKey, dateStamp, region, strings.Join(signedHeaders, ";"), signature)
	req.Header.Set("Authorization", authHeader)

	// Now validate it
	err := sig.ValidateRequest(req)
	if err != nil {
		t.Errorf("signature validation failed: %v", err)
		t.Logf("Canonical request:\n%s", canonicalRequest)
		t.Logf("String to sign:\n%s", stringToSign)
		t.Logf("Signature: %s", signature)
	}
}
