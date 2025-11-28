package storage

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Object represents an S3 object
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
	ContentType  string
	ETag         string
}

// Storage handles file operations on the local filesystem
type Storage struct {
	basePath string
	bucket   string
	mu       sync.RWMutex
}

// NewStorage creates a new storage instance
func NewStorage(basePath, bucket string) (*Storage, error) {
	// Create bucket directory if it doesn't exist
	bucketPath := filepath.Join(basePath, bucket)
	if err := os.MkdirAll(bucketPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create bucket directory: %w", err)
	}

	return &Storage{
		basePath: basePath,
		bucket:   bucket,
	}, nil
}

// GetObject retrieves an object from storage
func (s *Storage) GetObject(key string) (*Object, io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.keyToPath(key)

	// Check for path traversal
	if err := s.validatePath(path); err != nil {
		return nil, nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return nil, nil, ErrNotFound
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}

	obj := &Object{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime(),
		ContentType:  guessContentType(key),
		ETag:         generateETag(info),
	}

	return obj, file, nil
}

// HeadObject retrieves object metadata without the body
func (s *Storage) HeadObject(key string) (*Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.keyToPath(key)

	if err := s.validatePath(path); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if info.IsDir() {
		return nil, ErrNotFound
	}

	return &Object{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime(),
		ContentType:  guessContentType(key),
		ETag:         generateETag(info),
	}, nil
}

// PutObject stores an object
func (s *Storage) PutObject(key string, contentType string, body io.Reader) (*Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.keyToPath(key)

	if err := s.validatePath(path); err != nil {
		return nil, err
	}

	// Create parent directories
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directories: %w", err)
	}

	// Create the file
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Copy content
	size, err := io.Copy(file, body)
	if err != nil {
		_ = os.Remove(path) // Clean up on error
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Get file info for response
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	// Determine content type
	if contentType == "" {
		contentType = guessContentType(key)
	}

	return &Object{
		Key:          key,
		Size:         size,
		LastModified: info.ModTime(),
		ContentType:  contentType,
		ETag:         generateETag(info),
	}, nil
}

// DeleteObject removes an object from storage
func (s *Storage) DeleteObject(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.keyToPath(key)

	if err := s.validatePath(path); err != nil {
		return err
	}

	// Check if it exists
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// S3 returns success even if object doesn't exist
			return nil
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Try to clean up empty parent directories
	s.cleanEmptyDirs(filepath.Dir(path))

	return nil
}

// ListObjects returns all objects in the bucket
func (s *Storage) ListObjects(prefix string) ([]Object, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bucketPath := filepath.Join(s.basePath, s.bucket)
	var objects []Object

	err := filepath.Walk(bucketPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip the bucket directory itself
		if path == bucketPath {
			return nil
		}

		// Get relative path as key
		relPath, err := filepath.Rel(bucketPath, path)
		if err != nil {
			return err
		}

		// Convert to forward slashes for S3 compatibility
		key := filepath.ToSlash(relPath)

		// Apply prefix filter if provided
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}

		// For directories, add trailing slash (S3 folder convention)
		if info.IsDir() {
			key = key + "/"
		}

		objects = append(objects, Object{
			Key:          key,
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ContentType:  guessContentType(key),
			ETag:         generateETag(info),
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	return objects, nil
}

// EnsurePublicDir creates the public directory if it doesn't exist
func (s *Storage) EnsurePublicDir(prefix string) error {
	if prefix == "" {
		return nil
	}
	// Remove trailing slash for directory creation
	prefix = strings.TrimSuffix(prefix, "/")
	publicPath := filepath.Join(s.basePath, s.bucket, prefix)
	return os.MkdirAll(publicPath, 0755)
}

// keyToPath converts an S3 key to a filesystem path
func (s *Storage) keyToPath(key string) string {
	// Remove leading slash if present
	key = strings.TrimPrefix(key, "/")
	return filepath.Join(s.basePath, s.bucket, filepath.FromSlash(key))
}

// validatePath checks for path traversal attacks
func (s *Storage) validatePath(path string) error {
	bucketPath := filepath.Join(s.basePath, s.bucket)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	absBucket, err := filepath.Abs(bucketPath)
	if err != nil {
		return fmt.Errorf("invalid bucket path: %w", err)
	}

	if !strings.HasPrefix(absPath, absBucket) {
		return ErrInvalidPath
	}
	return nil
}

// cleanEmptyDirs removes empty parent directories up to the bucket root
func (s *Storage) cleanEmptyDirs(dir string) {
	bucketPath := filepath.Join(s.basePath, s.bucket)

	for dir != bucketPath {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		_ = os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// guessContentType guesses the MIME type from file extension
func guessContentType(key string) string {
	ext := filepath.Ext(key)
	if ext == "" {
		return "application/octet-stream"
	}

	// Handle types that have inconsistent system MIME database entries across platforms
	// (e.g., .xml returns "text/xml" on Linux but may differ on macOS)
	switch strings.ToLower(ext) {
	case ".xml":
		return "application/xml"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".webm":
		return "video/webm"
	}

	// Check standard MIME types from system database
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		return mimeType
	}

	// Fallback for common types not always in the default database
	switch strings.ToLower(ext) {
	case ".json":
		return "application/json"
	case ".js":
		return "application/javascript"
	case ".css":
		return "text/css"
	case ".html", ".htm":
		return "text/html"
	case ".txt":
		return "text/plain"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".mp4":
		return "video/mp4"
	case ".mp3":
		return "audio/mpeg"
	default:
		return "application/octet-stream"
	}
}

// generateETag generates a simple ETag based on file modification time and size
func generateETag(info os.FileInfo) string {
	// Simple ETag: use modification time and size
	// For a proper implementation, you'd compute MD5 of the content
	return fmt.Sprintf("\"%x-%x\"", info.ModTime().UnixNano(), info.Size())
}

// Errors
var (
	ErrNotFound    = fmt.Errorf("object not found")
	ErrInvalidPath = fmt.Errorf("invalid path")
)
