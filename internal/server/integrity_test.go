package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func putSigned(t *testing.T, srv *Server, key, body, payloadHash string) *http.Response {
	t.Helper()
	cfg := srv.config
	req := httptest.NewRequest(http.MethodPut, "/test-bucket/"+key, strings.NewReader(body))
	req.Host = "localhost:9000"
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)
	w := httptest.NewRecorder()
	srv.handleRequest(w, req)
	return w.Result()
}

func getContent(t *testing.T, srv *Server, key string) (int, string) {
	t.Helper()
	cfg := srv.config
	req := httptest.NewRequest(http.MethodGet, "/test-bucket/"+key, nil)
	req.Host = "localhost:9000"
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)
	w := httptest.NewRecorder()
	srv.handleRequest(w, req)
	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func TestPutObject_BodyMustMatchSignedHash(t *testing.T) {
	srv, err := NewServer(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if resp := putSigned(t, srv, "logo.png", "original", hashSHA256("original")); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected the first upload to pass, got %d", resp.StatusCode)
	}

	// A signed request replayed with another body.
	resp := putSigned(t, srv, "logo.png", "replaced", hashSHA256("original"))
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "XAmzContentSHA256Mismatch") {
		t.Fatalf("expected 400 XAmzContentSHA256Mismatch, got %d: %s", resp.StatusCode, body)
	}

	if status, content := getContent(t, srv, "logo.png"); status != http.StatusOK || content != "original" {
		t.Fatalf("expected the original content to survive, got %d %q", status, content)
	}
}

func TestPutObject_UnsignedPayloadIsNotChecked(t *testing.T) {
	srv, err := NewServer(testConfig(t))
	if err != nil {
		t.Fatal(err)
	}

	if resp := putSigned(t, srv, "free.txt", "any body", "UNSIGNED-PAYLOAD"); resp.StatusCode != http.StatusOK {
		t.Fatalf("expected an unsigned payload to be accepted, got %d", resp.StatusCode)
	}
	if _, content := getContent(t, srv, "free.txt"); content != "any body" {
		t.Fatalf("expected the body to be stored, got %q", content)
	}
}

func TestPutObject_OversizedBodyWithoutLengthIsRefused(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxFileSize = 16
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	body := strings.Repeat("x", 64)
	req := httptest.NewRequest(http.MethodPut, "/test-bucket/big.bin", strings.NewReader(body))
	req.Host = "localhost:9000"
	req.ContentLength = -1 // a chunked upload announces no length
	signRequest(req, cfg.AccessKey, cfg.SecretKey, cfg.Region)
	w := httptest.NewRecorder()
	srv.handleRequest(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
	if status, _ := getContent(t, srv, "big.bin"); status != http.StatusNotFound {
		t.Fatalf("expected nothing stored, got status %d", status)
	}
}
