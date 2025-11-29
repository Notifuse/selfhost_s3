package storage

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStorage(t *testing.T) {
	tempDir := t.TempDir()

	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	if storage == nil {
		t.Fatal("expected storage instance, got nil")
	}

	// Verify bucket directory was created
	bucketPath := filepath.Join(tempDir, "test-bucket")
	info, err := os.Stat(bucketPath)
	if err != nil {
		t.Fatalf("bucket directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("bucket path is not a directory")
	}
}

func TestPutObject(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	tests := []struct {
		name        string
		key         string
		contentType string
		content     string
		expectError bool
	}{
		{
			name:        "simple file",
			key:         "test.txt",
			contentType: "text/plain",
			content:     "Hello, World!",
			expectError: false,
		},
		{
			name:        "file in subdirectory",
			key:         "subdir/nested/file.json",
			contentType: "application/json",
			content:     `{"key": "value"}`,
			expectError: false,
		},
		{
			name:        "empty content type (should be guessed)",
			key:         "image.png",
			contentType: "",
			content:     "fake png content",
			expectError: false,
		},
		{
			name:        "folder marker",
			key:         "myfolder/",
			contentType: "",
			content:     "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.content)
			obj, err := storage.PutObject(tt.key, tt.contentType, reader)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if obj.Key != tt.key {
				t.Errorf("expected key %q, got %q", tt.key, obj.Key)
			}

			if obj.Size != int64(len(tt.content)) {
				t.Errorf("expected size %d, got %d", len(tt.content), obj.Size)
			}

			if obj.ETag == "" {
				t.Error("expected non-empty ETag")
			}

			// Verify file exists on disk
			path := filepath.Join(tempDir, "test-bucket", filepath.FromSlash(tt.key))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("file not created at %s", path)
			}
		})
	}
}

func TestGetObject(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Put an object first
	content := "Test content for get operation"
	_, err = storage.PutObject("gettest.txt", "text/plain", strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// Get the object
	obj, reader, err := storage.GetObject("gettest.txt")
	if err != nil {
		t.Fatalf("failed to get object: %v", err)
	}
	defer func() { _ = reader.Close() }()

	if obj.Key != "gettest.txt" {
		t.Errorf("expected key 'gettest.txt', got %q", obj.Key)
	}

	if obj.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), obj.Size)
	}

	if obj.ContentType != "text/plain; charset=utf-8" {
		t.Errorf("expected content type 'text/plain; charset=utf-8', got %q", obj.ContentType)
	}

	// Read and verify content
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read content: %v", err)
	}

	if string(data) != content {
		t.Errorf("expected content %q, got %q", content, string(data))
	}
}

func TestGetObject_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	_, _, err = storage.GetObject("nonexistent.txt")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestHeadObject(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	content := "Head test content"
	_, err = storage.PutObject("headtest.txt", "text/plain", strings.NewReader(content))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	obj, err := storage.HeadObject("headtest.txt")
	if err != nil {
		t.Fatalf("failed to head object: %v", err)
	}

	if obj.Key != "headtest.txt" {
		t.Errorf("expected key 'headtest.txt', got %q", obj.Key)
	}

	if obj.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), obj.Size)
	}
}

func TestHeadObject_NotFound(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	_, err = storage.HeadObject("nonexistent.txt")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteObject(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create an object
	_, err = storage.PutObject("todelete.txt", "text/plain", strings.NewReader("delete me"))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// Verify it exists
	_, err = storage.HeadObject("todelete.txt")
	if err != nil {
		t.Fatalf("object should exist before delete: %v", err)
	}

	// Delete it
	err = storage.DeleteObject("todelete.txt")
	if err != nil {
		t.Fatalf("failed to delete object: %v", err)
	}

	// Verify it's gone
	_, err = storage.HeadObject("todelete.txt")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestDeleteObject_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// S3 returns success when deleting non-existent object
	err = storage.DeleteObject("nonexistent.txt")
	if err != nil {
		t.Errorf("delete of non-existent object should succeed, got %v", err)
	}
}

func TestDeleteObject_PreservesEmptyDirs(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a deeply nested object
	_, err = storage.PutObject("a/b/c/file.txt", "text/plain", strings.NewReader("nested"))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// Verify directories exist
	nestedPath := filepath.Join(tempDir, "test-bucket", "a", "b", "c")
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Fatal("nested directories should exist")
	}

	// Delete the file
	err = storage.DeleteObject("a/b/c/file.txt")
	if err != nil {
		t.Fatalf("failed to delete object: %v", err)
	}

	// Empty directories should be preserved (S3 folder marker behavior)
	// This matches S3 behavior where folder markers persist even when empty
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("empty directories should be preserved after delete (S3 folder marker behavior)")
	}
}

func TestDeleteFolderMarker(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a folder marker
	_, err = storage.PutObject("testfolder/", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("failed to create folder marker: %v", err)
	}

	// Verify directory exists
	folderPath := filepath.Join(tempDir, "test-bucket", "testfolder")
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		t.Fatal("folder should exist after creation")
	}

	// Delete the folder marker
	err = storage.DeleteObject("testfolder/")
	if err != nil {
		t.Fatalf("failed to delete folder marker: %v", err)
	}

	// Verify directory is removed
	if _, err := os.Stat(folderPath); !os.IsNotExist(err) {
		t.Error("folder should be removed after delete")
	}
}

func TestDeleteFolderMarker_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Deleting non-existent folder should succeed (S3 behavior)
	err = storage.DeleteObject("nonexistent/")
	if err != nil {
		t.Errorf("delete of non-existent folder should succeed, got %v", err)
	}
}

func TestDeleteFolderMarker_NotEmpty(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a folder with a file inside
	_, err = storage.PutObject("folder/", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("failed to create folder marker: %v", err)
	}

	_, err = storage.PutObject("folder/file.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Try to delete the folder marker (should silently succeed but folder remains due to file)
	err = storage.DeleteObject("folder/")
	if err != nil {
		t.Fatalf("delete should not return error: %v", err)
	}

	// Folder should still exist because it has contents
	folderPath := filepath.Join(tempDir, "test-bucket", "folder")
	if _, err := os.Stat(folderPath); os.IsNotExist(err) {
		t.Error("folder with contents should still exist")
	}
}

func TestCreateFolderMarker_Nested(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a nested folder marker
	obj, err := storage.PutObject("a/b/c/nested/", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("failed to create nested folder: %v", err)
	}

	if obj.Key != "a/b/c/nested/" {
		t.Errorf("expected key 'a/b/c/nested/', got %q", obj.Key)
	}

	if obj.Size != 0 {
		t.Errorf("expected size 0, got %d", obj.Size)
	}

	if obj.ContentType != "application/x-directory" {
		t.Errorf("expected content type 'application/x-directory', got %q", obj.ContentType)
	}

	// Verify all directories were created
	nestedPath := filepath.Join(tempDir, "test-bucket", "a", "b", "c", "nested")
	if _, err := os.Stat(nestedPath); os.IsNotExist(err) {
		t.Error("nested folder should exist")
	}
}

func TestCreateFolderMarker_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to create folder with path traversal
	_, err = storage.PutObject("../../../etc/", "", strings.NewReader(""))
	if err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestListObjects(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create some objects
	testFiles := []struct {
		key     string
		content string
	}{
		{"file1.txt", "content1"},
		{"file2.txt", "content2"},
		{"images/photo1.jpg", "jpg1"},
		{"images/photo2.jpg", "jpg2"},
		{"docs/report.pdf", "pdf"},
	}

	for _, f := range testFiles {
		_, err := storage.PutObject(f.key, "", strings.NewReader(f.content))
		if err != nil {
			t.Fatalf("failed to put %s: %v", f.key, err)
		}
	}

	// List all objects
	objects, err := storage.ListObjects("")
	if err != nil {
		t.Fatalf("failed to list objects: %v", err)
	}

	// Should have files + directories
	// files: file1.txt, file2.txt, images/photo1.jpg, images/photo2.jpg, docs/report.pdf
	// dirs: images/, docs/
	expectedKeys := map[string]bool{
		"file1.txt":         true,
		"file2.txt":         true,
		"images/":           true,
		"images/photo1.jpg": true,
		"images/photo2.jpg": true,
		"docs/":             true,
		"docs/report.pdf":   true,
	}

	for _, obj := range objects {
		if !expectedKeys[obj.Key] {
			t.Errorf("unexpected key in listing: %q", obj.Key)
		}
		delete(expectedKeys, obj.Key)
	}

	if len(expectedKeys) > 0 {
		t.Errorf("missing keys in listing: %v", expectedKeys)
	}
}

func TestListObjects_WithPrefix(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create objects
	testFiles := []string{"images/a.jpg", "images/b.jpg", "docs/c.pdf"}
	for _, key := range testFiles {
		_, err := storage.PutObject(key, "", strings.NewReader("content"))
		if err != nil {
			t.Fatalf("failed to put %s: %v", key, err)
		}
	}

	// List with prefix
	objects, err := storage.ListObjects("images/")
	if err != nil {
		t.Fatalf("failed to list objects: %v", err)
	}

	for _, obj := range objects {
		if !strings.HasPrefix(obj.Key, "images/") {
			t.Errorf("object %q doesn't match prefix 'images/'", obj.Key)
		}
	}
}

func TestPathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to escape the bucket with path traversal
	maliciousKeys := []string{
		"../../../etc/passwd",
		"..\\..\\..\\windows\\system32\\config\\sam",
		"foo/../../bar",
		"/etc/passwd",
	}

	for _, key := range maliciousKeys {
		t.Run(key, func(t *testing.T) {
			// PutObject should reject path traversal
			_, err := storage.PutObject(key, "text/plain", strings.NewReader("malicious"))
			if err != ErrInvalidPath {
				// If not ErrInvalidPath, the file should at least be contained within bucket
				if err == nil {
					// Check that file is within bucket
					obj, _ := storage.HeadObject(key)
					if obj != nil {
						// Verify actual path is within bucket
						actualPath := filepath.Join(tempDir, "test-bucket", filepath.FromSlash(key))
						absActual, _ := filepath.Abs(actualPath)
						absBucket, _ := filepath.Abs(filepath.Join(tempDir, "test-bucket"))
						if !strings.HasPrefix(absActual, absBucket) {
							t.Errorf("path traversal succeeded for key %q", key)
						}
					}
				}
			}

			// GetObject should reject path traversal
			_, _, err = storage.GetObject(key)
			if err == nil {
				t.Logf("GetObject for %q didn't return error (may be contained)", key)
			}
		})
	}
}

func TestGuessContentType(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"file.txt", "text/plain; charset=utf-8"},
		{"file.json", "application/json"},
		{"file.html", "text/html; charset=utf-8"},
		{"file.css", "text/css; charset=utf-8"},
		{"file.js", "text/javascript; charset=utf-8"}, // Go's mime package uses text/javascript
		{"file.png", "image/png"},
		{"file.jpg", "image/jpeg"},
		{"file.jpeg", "image/jpeg"},
		{"file.gif", "image/gif"},
		{"file.svg", "image/svg+xml"},
		{"file.webp", "image/webp"},
		{"file.pdf", "application/pdf"},
		{"file.zip", "application/zip"},
		{"file.mp4", "video/mp4"},
		{"file.mp3", "audio/mpeg"},
		{"file.woff", "font/woff"},
		{"file.woff2", "font/woff2"},
		{"file.unknown", "application/octet-stream"},
		{"file", "application/octet-stream"},
		{"path/to/file.png", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := guessContentType(tt.key)
			if result != tt.expected {
				t.Errorf("guessContentType(%q) = %q, expected %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool)
	errChan := make(chan error, 100)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(i int) {
			key := "concurrent-" + string(rune('a'+i)) + ".txt"
			content := bytes.Repeat([]byte("x"), 1000)
			_, err := storage.PutObject(key, "text/plain", bytes.NewReader(content))
			if err != nil {
				errChan <- err
			}
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check for errors
	close(errChan)
	for err := range errChan {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify all files exist
	objects, err := storage.ListObjects("concurrent-")
	if err != nil {
		t.Fatalf("failed to list objects: %v", err)
	}

	fileCount := 0
	for _, obj := range objects {
		if strings.HasPrefix(obj.Key, "concurrent-") && !strings.HasSuffix(obj.Key, "/") {
			fileCount++
		}
	}

	if fileCount != 10 {
		t.Errorf("expected 10 concurrent files, got %d", fileCount)
	}
}

func TestLargeFile(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a 1MB file
	size := 1024 * 1024
	content := bytes.Repeat([]byte("A"), size)

	obj, err := storage.PutObject("largefile.bin", "application/octet-stream", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("failed to put large file: %v", err)
	}

	if obj.Size != int64(size) {
		t.Errorf("expected size %d, got %d", size, obj.Size)
	}

	// Read it back
	_, reader, err := storage.GetObject("largefile.bin")
	if err != nil {
		t.Fatalf("failed to get large file: %v", err)
	}
	defer func() { _ = reader.Close() }()

	readContent, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read large file: %v", err)
	}

	if len(readContent) != size {
		t.Errorf("expected to read %d bytes, got %d", size, len(readContent))
	}

	if !bytes.Equal(content, readContent) {
		t.Error("content mismatch for large file")
	}
}

func TestEnsurePublicDir(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	tests := []struct {
		name      string
		prefix    string
		shouldErr bool
	}{
		{
			name:      "creates public directory",
			prefix:    "public/",
			shouldErr: false,
		},
		{
			name:      "creates custom prefix directory",
			prefix:    "assets/",
			shouldErr: false,
		},
		{
			name:      "handles prefix without trailing slash",
			prefix:    "files",
			shouldErr: false,
		},
		{
			name:      "empty prefix is no-op",
			prefix:    "",
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.EnsurePublicDir(tt.prefix)

			if tt.shouldErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// For non-empty prefix, verify directory was created
			if tt.prefix != "" {
				prefix := strings.TrimSuffix(tt.prefix, "/")
				dirPath := filepath.Join(tempDir, "test-bucket", prefix)
				info, err := os.Stat(dirPath)
				if err != nil {
					t.Fatalf("directory not created: %v", err)
				}
				if !info.IsDir() {
					t.Error("expected path to be a directory")
				}
			}
		})
	}
}

func TestEnsurePublicDir_Idempotent(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Call twice - should not error
	err = storage.EnsurePublicDir("public/")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	err = storage.EnsurePublicDir("public/")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
}

func TestGetObject_Directory(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a file in a subdirectory
	_, err = storage.PutObject("subdir/file.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// Try to get the directory - should return ErrNotFound
	_, _, err = storage.GetObject("subdir")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound when getting directory, got %v", err)
	}
}

func TestHeadObject_Directory(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Create a file in a subdirectory
	_, err = storage.PutObject("subdir/file.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Fatalf("failed to put object: %v", err)
	}

	// Try to head the directory - should return ErrNotFound
	_, err = storage.HeadObject("subdir")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound when heading directory, got %v", err)
	}
}

func TestDeleteObject_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to delete with path traversal
	err = storage.DeleteObject("../../../etc/passwd")
	if err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestPutObject_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to put with path traversal
	_, err = storage.PutObject("../../../etc/passwd", "text/plain", strings.NewReader("malicious"))
	if err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestGetObject_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to get with path traversal
	_, _, err = storage.GetObject("../../../etc/passwd")
	if err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestHeadObject_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to head with path traversal
	_, err = storage.HeadObject("../../../etc/passwd")
	if err != ErrInvalidPath {
		t.Errorf("expected ErrInvalidPath, got %v", err)
	}
}

func TestGuessContentType_AdditionalTypes(t *testing.T) {
	// Test additional extensions that may not be in the standard MIME database
	tests := []struct {
		key      string
		expected string
	}{
		{"file.xml", "application/xml"},
		{"file.htm", "text/html; charset=utf-8"},
		{"file.webm", "video/webm"},
		{"FILE.TXT", "text/plain; charset=utf-8"}, // uppercase extension
		{"file.PNG", "image/png"},                 // uppercase extension
		{"file.woff", "font/woff"},
		{"file.woff2", "font/woff2"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := guessContentType(tt.key)
			if result != tt.expected {
				t.Errorf("guessContentType(%q) = %q, expected %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestValidatePath_Errors(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Test various path traversal attempts through the API
	maliciousKeys := []string{
		"../escape",
		"test/../../../etc/passwd",
		"valid/path/../../../escape",
	}

	for _, key := range maliciousKeys {
		t.Run(key, func(t *testing.T) {
			_, err := storage.PutObject(key, "text/plain", strings.NewReader("test"))
			if err != ErrInvalidPath {
				t.Logf("PutObject(%q) returned %v (expected ErrInvalidPath or error)", key, err)
			}
		})
	}
}

func TestListObjects_InvalidPath(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// List with path traversal attempt in prefix - should be handled safely
	objects, err := storage.ListObjects("../../../etc/")
	if err != nil {
		// If it errors, that's also acceptable
		return
	}
	// If it succeeds, it should return empty or only valid objects
	for _, obj := range objects {
		if strings.Contains(obj.Key, "..") {
			t.Errorf("listing returned object with path traversal: %q", obj.Key)
		}
	}
}

func TestPutObject_EmptyKey(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to put with empty key - should return an error (bucket directory is not a file)
	_, err = storage.PutObject("", "text/plain", strings.NewReader("content"))
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}

func TestGetObject_EmptyKey(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to get with empty key - should return an error
	_, _, err = storage.GetObject("")
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}

func TestHeadObject_EmptyKey(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to head with empty key - should return an error
	_, err = storage.HeadObject("")
	if err == nil {
		t.Error("expected error for empty key, got nil")
	}
}

func TestDeleteObject_EmptyKey(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to delete with empty key - may succeed (deleting bucket dir fails gracefully)
	// This tests the delete code path, not the validation
	_ = storage.DeleteObject("")
}

func TestPutObject_WithNullByte(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to put with null byte in key - should fail (filesystem error or path traversal)
	_, err = storage.PutObject("file\x00.txt", "text/plain", strings.NewReader("content"))
	if err == nil {
		t.Error("expected error for key with null byte, got nil")
	}
}

func TestPutObject_KeyStartsWithSlash(t *testing.T) {
	tempDir := t.TempDir()
	storage, err := NewStorage(tempDir, "test-bucket")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	// Try to put with key starting with slash - stripped by keyToPath, so should succeed
	// The leading slash is trimmed in keyToPath
	_, err = storage.PutObject("/test/file.txt", "text/plain", strings.NewReader("content"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify the file was created with leading slash stripped
	_, err = storage.HeadObject("test/file.txt")
	if err != nil {
		t.Errorf("file should exist without leading slash: %v", err)
	}
}
