package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Notifuse/selfhost_s3/internal/config"
)

// testConfig creates a config for testing
func testConfig(t *testing.T) *config.Config {
	return &config.Config{
		Bucket:            "test-bucket",
		AccessKey:         "test-access-key",
		SecretKey:         "test-secret-key",
		Port:              9000,
		StoragePath:       t.TempDir(),
		Region:            "us-east-1",
		CORSOrigins:       []string{"*"},
		MaxFileSize:       10 * 1024 * 1024, // 10MB
		PublicPrefix:      "public/",
		PublicCacheMaxAge: 31536000,
	}
}

// signRequest adds AWS Signature V4 authentication to a request
func signRequest(req *http.Request, accessKey, secretKey, region string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	req.Header.Set("X-Amz-Date", amzDate)

	// Get or set payload hash
	payloadHash := req.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	}

	// Determine signed headers
	signedHeadersList := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if req.Header.Get("Content-Type") != "" {
		signedHeadersList = append([]string{"content-type"}, signedHeadersList...)
	}

	// Build canonical headers
	canonicalHeaders := ""
	for _, h := range signedHeadersList {
		var val string
		if h == "host" {
			val = req.Host
		} else {
			val = req.Header.Get(h)
		}
		canonicalHeaders += fmt.Sprintf("%s:%s\n", strings.ToLower(h), strings.TrimSpace(val))
	}

	signedHeaders := strings.Join(signedHeadersList, ";")

	// Build canonical request
	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// Build canonical query string (must be sorted and URL encoded)
	canonicalQueryString := buildCanonicalQueryString(req.URL.Query())

	canonicalRequest := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		req.Method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
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
		accessKey, dateStamp, region, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)
}

// buildCanonicalQueryString creates the canonical query string per AWS spec
func buildCanonicalQueryString(query url.Values) string {
	if len(query) == 0 {
		return ""
	}

	// Sort parameter names
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical query string
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		values := query[k]
		sort.Strings(values)
		for _, v := range values {
			pairs = append(pairs, fmt.Sprintf("%s=%s",
				url.QueryEscape(k),
				url.QueryEscape(v),
			))
		}
	}

	return strings.Join(pairs, "&")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hashSHA256(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func TestHealthEndpoint(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Errorf("expected body {\"status\":\"ok\"}, got %q", string(body))
	}

	if resp.Header.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", resp.Header.Get("Content-Type"))
	}
}

func TestCORSMiddleware(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create a simple handler to wrap
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := srv.corsMiddleware(handler)

	// Test preflight request
	req := httptest.NewRequest(http.MethodOptions, "/test-bucket/key", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for preflight, got %d", resp.StatusCode)
	}

	// Check CORS headers
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("missing Access-Control-Allow-Origin header")
	}
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("missing Access-Control-Allow-Methods header")
	}
	if resp.Header.Get("Access-Control-Allow-Headers") == "" {
		t.Error("missing Access-Control-Allow-Headers header")
	}
}

func TestCORSMiddleware_SpecificOrigins(t *testing.T) {
	cfg := testConfig(t)
	cfg.CORSOrigins = []string{"https://example.com", "https://app.example.com"}

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := srv.corsMiddleware(handler)

	// Test with allowed origin
	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	allowedOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowedOrigin != "https://example.com" {
		t.Errorf("expected origin https://example.com, got %q", allowedOrigin)
	}
}

func TestHandleRequest_Unauthorized(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Request without auth
	req := httptest.NewRequest(http.MethodGet, "/test-bucket/key", nil)
	req.Host = "localhost:9000"
	w := httptest.NewRecorder()

	srv.handleRequest(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", resp.StatusCode)
	}

	// Verify XML error response
	var errResp ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "AccessDenied" {
		t.Errorf("expected error code 'AccessDenied', got %q", errResp.Code)
	}
}

func TestHandleRequest_WrongBucket(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/wrong-bucket/key", nil)
	req.Host = "localhost:9000"
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	w := httptest.NewRecorder()
	srv.handleRequest(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "NoSuchBucket" {
		t.Errorf("expected error code 'NoSuchBucket', got %q", errResp.Code)
	}
}

func TestPutAndGetObject(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "Hello, SelfhostS3!"

	// PUT object
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/hello.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	putReq.Header.Set("Content-Type", "text/plain")
	putReq.Header.Set("X-Amz-Content-Sha256", hashSHA256(content))
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	putResp := putW.Result()
	defer func() { _ = putResp.Body.Close() }()

	if putResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(putResp.Body)
		t.Fatalf("PUT failed with status %d: %s", putResp.StatusCode, string(body))
	}

	if putResp.Header.Get("ETag") == "" {
		t.Error("PUT response missing ETag header")
	}

	// GET object
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/hello.txt", nil)
	getReq.Host = "localhost:9000"
	signRequest(getReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("GET failed with status %d: %s", getResp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(getResp.Body)
	if string(body) != content {
		t.Errorf("expected content %q, got %q", content, string(body))
	}

	if getResp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Errorf("expected Content-Type text/plain; charset=utf-8, got %q", getResp.Header.Get("Content-Type"))
	}

	if getResp.Header.Get("ETag") == "" {
		t.Error("GET response missing ETag header")
	}
}

func TestHeadObject(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "Test content for HEAD"

	// PUT object first
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/headtest.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	putReq.Header.Set("Content-Type", "text/plain")
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// HEAD object
	headReq := httptest.NewRequest(http.MethodHead, "/test-bucket/headtest.txt", nil)
	headReq.Host = "localhost:9000"
	signRequest(headReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	headW := httptest.NewRecorder()
	srv.handleRequest(headW, headReq)

	headResp := headW.Result()
	defer func() { _ = headResp.Body.Close() }()

	if headResp.StatusCode != http.StatusOK {
		t.Errorf("HEAD failed with status %d", headResp.StatusCode)
	}

	if headResp.Header.Get("Content-Length") != fmt.Sprintf("%d", len(content)) {
		t.Errorf("expected Content-Length %d, got %q", len(content), headResp.Header.Get("Content-Length"))
	}

	// HEAD should return no body
	body, _ := io.ReadAll(headResp.Body)
	if len(body) != 0 {
		t.Errorf("HEAD should return empty body, got %d bytes", len(body))
	}
}

func TestDeleteObject(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// PUT object
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/todelete.txt", strings.NewReader("delete me"))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// DELETE object
	delReq := httptest.NewRequest(http.MethodDelete, "/test-bucket/todelete.txt", nil)
	delReq.Host = "localhost:9000"
	signRequest(delReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	delW := httptest.NewRecorder()
	srv.handleRequest(delW, delReq)

	delResp := delW.Result()
	defer func() { _ = delResp.Body.Close() }()

	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE expected status 204, got %d", delResp.StatusCode)
	}

	// GET should now fail
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/todelete.txt", nil)
	getReq.Host = "localhost:9000"
	signRequest(getReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	if getW.Result().StatusCode != http.StatusNotFound {
		t.Errorf("GET after DELETE expected status 404, got %d", getW.Result().StatusCode)
	}
}

func TestListObjectsV2(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create some objects
	files := []string{"file1.txt", "file2.txt", "dir/file3.txt"}
	for _, f := range files {
		req := httptest.NewRequest(http.MethodPut, "/test-bucket/"+f, strings.NewReader("content"))
		req.Host = "localhost:9000"
		signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

		w := httptest.NewRecorder()
		srv.handleRequest(w, req)

		if w.Result().StatusCode != http.StatusOK {
			t.Fatalf("PUT %s failed with status %d", f, w.Result().StatusCode)
		}
	}

	// List objects
	listReq := httptest.NewRequest(http.MethodGet, "/test-bucket?list-type=2", nil)
	listReq.Host = "localhost:9000"
	signRequest(listReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	listW := httptest.NewRecorder()
	srv.handleRequest(listW, listReq)

	listResp := listW.Result()
	defer func() { _ = listResp.Body.Close() }()

	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("LIST failed with status %d: %s", listResp.StatusCode, string(body))
	}

	if listResp.Header.Get("Content-Type") != "application/xml" {
		t.Errorf("expected Content-Type application/xml, got %q", listResp.Header.Get("Content-Type"))
	}

	var result ListBucketResult
	if err := xml.NewDecoder(listResp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if result.Name != "test-bucket" {
		t.Errorf("expected bucket name 'test-bucket', got %q", result.Name)
	}

	// Should have files + the directory
	if len(result.Contents) < len(files) {
		t.Errorf("expected at least %d objects, got %d", len(files), len(result.Contents))
	}

	// Verify files are present
	keys := make(map[string]bool)
	for _, c := range result.Contents {
		keys[c.Key] = true
	}

	for _, f := range files {
		if !keys[f] {
			t.Errorf("missing file %q in listing", f)
		}
	}
}

func TestListObjectsV2_WithPrefix(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create objects
	files := []string{"images/a.jpg", "images/b.jpg", "docs/c.pdf"}
	for _, f := range files {
		req := httptest.NewRequest(http.MethodPut, "/test-bucket/"+f, strings.NewReader("content"))
		req.Host = "localhost:9000"
		signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

		w := httptest.NewRecorder()
		srv.handleRequest(w, req)
	}

	// List with prefix
	listReq := httptest.NewRequest(http.MethodGet, "/test-bucket?list-type=2&prefix=images/", nil)
	listReq.Host = "localhost:9000"
	signRequest(listReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	listW := httptest.NewRecorder()
	srv.handleRequest(listW, listReq)

	var result ListBucketResult
	if err := xml.NewDecoder(listW.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if result.Prefix != "images/" {
		t.Errorf("expected prefix 'images/', got %q", result.Prefix)
	}

	for _, c := range result.Contents {
		if !strings.HasPrefix(c.Key, "images/") {
			t.Errorf("object %q doesn't match prefix 'images/'", c.Key)
		}
	}
}

func TestGetObject_NotFound(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test-bucket/nonexistent.txt", nil)
	req.Host = "localhost:9000"
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	w := httptest.NewRecorder()
	srv.handleRequest(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "NoSuchKey" {
		t.Errorf("expected error code 'NoSuchKey', got %q", errResp.Code)
	}
}

func TestPutObject_TooLarge(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxFileSize = 100 // 100 bytes max

	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Try to upload a file larger than limit
	largeContent := strings.Repeat("x", 200)
	req := httptest.NewRequest(http.MethodPut, "/test-bucket/large.txt", strings.NewReader(largeContent))
	req.Host = "localhost:9000"
	req.ContentLength = int64(len(largeContent))
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	w := httptest.NewRecorder()
	srv.handleRequest(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("expected status 413, got %d", resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := xml.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "EntityTooLarge" {
		t.Errorf("expected error code 'EntityTooLarge', got %q", errResp.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/test-bucket/key", nil)
	req.Host = "localhost:9000"
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	w := httptest.NewRecorder()
	srv.handleRequest(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestContentTypePreserved(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// PUT with specific content type
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/data.bin", strings.NewReader("binary data"))
	putReq.Host = "localhost:9000"
	putReq.Header.Set("Content-Type", "application/octet-stream")
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// GET and check content type
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/data.bin", nil)
	getReq.Host = "localhost:9000"
	signRequest(getReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	// Since we're guessing from extension, .bin should give application/octet-stream
	ct := getResp.Header.Get("Content-Type")
	if ct != "application/octet-stream" {
		t.Errorf("expected Content-Type application/octet-stream, got %q", ct)
	}
}

func TestNestedPaths(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create deeply nested object
	key := "a/b/c/d/e/file.txt"
	content := "nested content"

	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/"+key, strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// GET it back
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/"+key, nil)
	getReq.Host = "localhost:9000"
	signRequest(getReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("GET failed with status %d", getResp.StatusCode)
	}

	body, _ := io.ReadAll(getResp.Body)
	if string(body) != content {
		t.Errorf("expected content %q, got %q", content, string(body))
	}
}

// Tests for public access feature

func TestPublicAccess_GetWithoutAuth(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "public file content"

	// PUT object to public folder (with auth)
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/image.png", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// GET without auth should succeed for public path
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/image.png", nil)
	getReq.Host = "localhost:9000"
	// No signRequest - testing without auth

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusOK {
		t.Errorf("public GET expected status 200, got %d", getResp.StatusCode)
	}

	body, _ := io.ReadAll(getResp.Body)
	if string(body) != content {
		t.Errorf("expected content %q, got %q", content, string(body))
	}
}

func TestPublicAccess_HeadWithoutAuth(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "public file for head"

	// PUT object to public folder (with auth)
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/headfile.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	if putW.Result().StatusCode != http.StatusOK {
		t.Fatalf("PUT failed with status %d", putW.Result().StatusCode)
	}

	// HEAD without auth should succeed for public path
	headReq := httptest.NewRequest(http.MethodHead, "/test-bucket/public/headfile.txt", nil)
	headReq.Host = "localhost:9000"
	// No signRequest - testing without auth

	headW := httptest.NewRecorder()
	srv.handleRequest(headW, headReq)

	headResp := headW.Result()
	defer func() { _ = headResp.Body.Close() }()

	if headResp.StatusCode != http.StatusOK {
		t.Errorf("public HEAD expected status 200, got %d", headResp.StatusCode)
	}
}

func TestPublicAccess_PutWithoutAuth_Fails(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// PUT without auth should fail even for public path
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/unauthorized.txt", strings.NewReader("content"))
	putReq.Host = "localhost:9000"
	// No signRequest - testing without auth

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	putResp := putW.Result()
	defer func() { _ = putResp.Body.Close() }()

	if putResp.StatusCode != http.StatusForbidden {
		t.Errorf("public PUT without auth expected status 403, got %d", putResp.StatusCode)
	}
}

func TestPublicAccess_DeleteWithoutAuth_Fails(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// First create a file with auth
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/todelete.txt", strings.NewReader("content"))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// DELETE without auth should fail even for public path
	delReq := httptest.NewRequest(http.MethodDelete, "/test-bucket/public/todelete.txt", nil)
	delReq.Host = "localhost:9000"
	// No signRequest - testing without auth

	delW := httptest.NewRecorder()
	srv.handleRequest(delW, delReq)

	delResp := delW.Result()
	defer func() { _ = delResp.Body.Close() }()

	if delResp.StatusCode != http.StatusForbidden {
		t.Errorf("public DELETE without auth expected status 403, got %d", delResp.StatusCode)
	}
}

func TestPublicAccess_NonPublicPath_RequiresAuth(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// First create a file with auth in non-public path
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/private/secret.txt", strings.NewReader("secret"))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET without auth should fail for non-public path
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/private/secret.txt", nil)
	getReq.Host = "localhost:9000"
	// No signRequest - testing without auth

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusForbidden {
		t.Errorf("non-public GET without auth expected status 403, got %d", getResp.StatusCode)
	}
}

func TestPublicAccess_CacheControlHeader(t *testing.T) {
	cfg := testConfig(t)
	cfg.PublicCacheMaxAge = 3600 // 1 hour for test
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "cached public content"

	// PUT object to public folder
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/cached.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET without auth
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/cached.txt", nil)
	getReq.Host = "localhost:9000"

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	cacheControl := getResp.Header.Get("Cache-Control")
	expectedCacheControl := "public, max-age=3600"
	if cacheControl != expectedCacheControl {
		t.Errorf("expected Cache-Control %q, got %q", expectedCacheControl, cacheControl)
	}
}

func TestPublicAccess_CacheControlDisabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.PublicCacheMaxAge = 0 // Disabled
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "no cache content"

	// PUT object to public folder
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/nocache.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET without auth
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/nocache.txt", nil)
	getReq.Host = "localhost:9000"

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	cacheControl := getResp.Header.Get("Cache-Control")
	if cacheControl != "" {
		t.Errorf("expected no Cache-Control header when disabled, got %q", cacheControl)
	}
}

func TestPublicAccess_DownloadParam(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "downloadable content"

	// PUT object to public folder
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/document.pdf", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET with download=1 parameter
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/document.pdf?download=1", nil)
	getReq.Host = "localhost:9000"

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	contentDisposition := getResp.Header.Get("Content-Disposition")
	expectedDisposition := `attachment; filename="document.pdf"`
	if contentDisposition != expectedDisposition {
		t.Errorf("expected Content-Disposition %q, got %q", expectedDisposition, contentDisposition)
	}
}

func TestPublicAccess_DownloadParam_NestedPath(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "nested downloadable"

	// PUT object to nested public folder
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/docs/reports/annual.pdf", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET with download=1 parameter - should use filename only, not full path
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/docs/reports/annual.pdf?download=1", nil)
	getReq.Host = "localhost:9000"

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	contentDisposition := getResp.Header.Get("Content-Disposition")
	expectedDisposition := `attachment; filename="annual.pdf"`
	if contentDisposition != expectedDisposition {
		t.Errorf("expected Content-Disposition %q, got %q", expectedDisposition, contentDisposition)
	}
}

func TestPublicAccess_Disabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.PublicPrefix = "" // Disable public access
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "should require auth"

	// PUT object with auth
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/file.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET without auth should fail when public access is disabled
	getReq := httptest.NewRequest(http.MethodGet, "/test-bucket/public/file.txt", nil)
	getReq.Host = "localhost:9000"
	// No signRequest

	getW := httptest.NewRecorder()
	srv.handleRequest(getW, getReq)

	getResp := getW.Result()
	defer func() { _ = getResp.Body.Close() }()

	if getResp.StatusCode != http.StatusForbidden {
		t.Errorf("when public access disabled, GET without auth expected status 403, got %d", getResp.StatusCode)
	}
}

func TestDeleteObject_InvalidPath(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// DELETE with path traversal attempt
	delReq := httptest.NewRequest(http.MethodDelete, "/test-bucket/../../../etc/passwd", nil)
	delReq.Host = "localhost:9000"
	signRequest(delReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	delW := httptest.NewRecorder()
	srv.handleRequest(delW, delReq)

	delResp := delW.Result()
	defer func() { _ = delResp.Body.Close() }()

	if delResp.StatusCode != http.StatusBadRequest {
		t.Errorf("DELETE with invalid path expected status 400, got %d", delResp.StatusCode)
	}

	var errResp ErrorResponse
	if err := xml.NewDecoder(delResp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "InvalidArgument" {
		t.Errorf("expected error code 'InvalidArgument', got %q", errResp.Code)
	}
}

func TestPutObject_InvalidPath(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// PUT with path traversal attempt
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/../../../etc/passwd", strings.NewReader("malicious"))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	putResp := putW.Result()
	defer func() { _ = putResp.Body.Close() }()

	if putResp.StatusCode != http.StatusBadRequest {
		t.Errorf("PUT with invalid path expected status 400, got %d", putResp.StatusCode)
	}

	var errResp ErrorResponse
	if err := xml.NewDecoder(putResp.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.Code != "InvalidArgument" {
		t.Errorf("expected error code 'InvalidArgument', got %q", errResp.Code)
	}
}

func TestHeadObject_NotFound(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// HEAD for non-existent object
	headReq := httptest.NewRequest(http.MethodHead, "/test-bucket/nonexistent.txt", nil)
	headReq.Host = "localhost:9000"
	signRequest(headReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	headW := httptest.NewRecorder()
	srv.handleRequest(headW, headReq)

	headResp := headW.Result()
	defer func() { _ = headResp.Body.Close() }()

	if headResp.StatusCode != http.StatusNotFound {
		t.Errorf("HEAD for non-existent object expected status 404, got %d", headResp.StatusCode)
	}
}

func TestPublicAccess_HeadCacheControlHeader(t *testing.T) {
	cfg := testConfig(t)
	cfg.PublicCacheMaxAge = 7200 // 2 hours
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	content := "cached public content"

	// PUT object to public folder
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/public/cachedhead.txt", strings.NewReader(content))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// HEAD without auth on public path
	headReq := httptest.NewRequest(http.MethodHead, "/test-bucket/public/cachedhead.txt", nil)
	headReq.Host = "localhost:9000"

	headW := httptest.NewRecorder()
	srv.handleRequest(headW, headReq)

	headResp := headW.Result()
	defer func() { _ = headResp.Body.Close() }()

	if headResp.StatusCode != http.StatusOK {
		t.Errorf("public HEAD expected status 200, got %d", headResp.StatusCode)
	}

	cacheControl := headResp.Header.Get("Cache-Control")
	expectedCacheControl := "public, max-age=7200"
	if cacheControl != expectedCacheControl {
		t.Errorf("expected Cache-Control %q, got %q", expectedCacheControl, cacheControl)
	}
}

func TestListObjectsV2_LegacyWithoutListType(t *testing.T) {
	cfg := testConfig(t)
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Create an object
	putReq := httptest.NewRequest(http.MethodPut, "/test-bucket/legacy-list.txt", strings.NewReader("content"))
	putReq.Host = "localhost:9000"
	signRequest(putReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	putW := httptest.NewRecorder()
	srv.handleRequest(putW, putReq)

	// GET bucket without key (legacy list)
	listReq := httptest.NewRequest(http.MethodGet, "/test-bucket/", nil)
	listReq.Host = "localhost:9000"
	signRequest(listReq, cfg.AccessKey, cfg.SecretKey, cfg.Region)

	listW := httptest.NewRecorder()
	srv.handleRequest(listW, listReq)

	listResp := listW.Result()
	defer func() { _ = listResp.Body.Close() }()

	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("legacy LIST failed with status %d: %s", listResp.StatusCode, string(body))
	}

	var result ListBucketResult
	if err := xml.NewDecoder(listResp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if result.Name != "test-bucket" {
		t.Errorf("expected bucket name 'test-bucket', got %q", result.Name)
	}
}
