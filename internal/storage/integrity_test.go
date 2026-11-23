package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func readAll(t *testing.T, s *Storage, key string) string {
	t.Helper()
	_, reader, err := s.GetObject(key)
	if err != nil {
		t.Fatalf("GetObject(%q): %v", key, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newIntegrityStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	base := t.TempDir()
	s, err := NewStorage(base, "bucket")
	if err != nil {
		t.Fatal(err)
	}
	return s, base
}

func TestPutObjectWithSHA256_Match(t *testing.T) {
	s, _ := newIntegrityStorage(t)

	if _, err := s.PutObjectWithSHA256("a.txt", "text/plain", strings.NewReader("hello"), sha256Hex("hello")); err != nil {
		t.Fatalf("expected a matching hash to be stored, got %v", err)
	}
	if got := readAll(t, s, "a.txt"); got != "hello" {
		t.Fatalf("expected %q, got %q", "hello", got)
	}
}

func TestPutObjectWithSHA256_UppercaseHash(t *testing.T) {
	s, _ := newIntegrityStorage(t)

	if _, err := s.PutObjectWithSHA256("a.txt", "", strings.NewReader("hello"), strings.ToUpper(sha256Hex("hello"))); err != nil {
		t.Fatalf("expected an uppercase hash to match, got %v", err)
	}
}

func TestPutObjectWithSHA256_MismatchKeepsPreviousContent(t *testing.T) {
	s, _ := newIntegrityStorage(t)
	if _, err := s.PutObject("logo.png", "", strings.NewReader("original")); err != nil {
		t.Fatal(err)
	}

	_, err := s.PutObjectWithSHA256("logo.png", "", strings.NewReader("replaced"), sha256Hex("something else"))
	if !errors.Is(err, ErrContentSHA256Mismatch) {
		t.Fatalf("expected ErrContentSHA256Mismatch, got %v", err)
	}
	if got := readAll(t, s, "logo.png"); got != "original" {
		t.Fatalf("expected the previous content to survive, got %q", got)
	}
}

func TestPutObjectWithSHA256_MismatchStoresNothing(t *testing.T) {
	s, _ := newIntegrityStorage(t)

	_, err := s.PutObjectWithSHA256("new.txt", "", strings.NewReader("body"), sha256Hex("other"))
	if !errors.Is(err, ErrContentSHA256Mismatch) {
		t.Fatalf("expected ErrContentSHA256Mismatch, got %v", err)
	}
	if _, err := s.HeadObject("new.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected nothing stored, got %v", err)
	}
}

// failingReader returns some bytes, then an error, like a client that disconnects.
type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("connection reset")
}

func TestPutObject_InterruptedUploadKeepsPreviousContent(t *testing.T) {
	s, _ := newIntegrityStorage(t)
	if _, err := s.PutObject("doc.txt", "", strings.NewReader("complete")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.PutObject("doc.txt", "", &failingReader{}); err == nil {
		t.Fatal("expected an interrupted upload to fail")
	}
	if got := readAll(t, s, "doc.txt"); got != "complete" {
		t.Fatalf("expected the previous content to survive, got %q", got)
	}
}

func TestPutObject_LeavesNoTemporaryFiles(t *testing.T) {
	s, base := newIntegrityStorage(t)
	if _, err := s.PutObject("ok.txt", "", strings.NewReader("ok")); err != nil {
		t.Fatal(err)
	}
	_, _ = s.PutObjectWithSHA256("bad.txt", "", strings.NewReader("bad"), sha256Hex("x"))
	_, _ = s.PutObject("cut.txt", "", &failingReader{})

	objects, err := s.ListObjects("")
	if err != nil {
		t.Fatal(err)
	}
	if len(objects) != 1 || objects[0].Key != "ok.txt" {
		keys := make([]string, len(objects))
		for i, o := range objects {
			keys[i] = o.Key
		}
		t.Fatalf("expected only ok.txt in the bucket, got %v", keys)
	}

	err = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Base(path) != "ok.txt" {
			t.Errorf("unexpected file left on disk: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNewStorage_RemovesUploadsLeftByAStop(t *testing.T) {
	base := t.TempDir()
	if _, err := NewStorage(base, "bucket"); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(base, tmpDirName, "upload-123")
	if err := os.WriteFile(stray, []byte("half"), 0600); err != nil {
		t.Fatal(err)
	}

	// A restart, after a stop in the middle of an upload.
	if _, err := NewStorage(base, "bucket"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("expected the leftover upload to be removed, got %v", err)
	}
}
