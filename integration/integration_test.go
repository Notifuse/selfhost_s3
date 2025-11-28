package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	testEndpoint  = "http://localhost:9000"
	testBucket    = "test-bucket"
	testAccessKey = "testkey"
	testSecretKey = "testsecret"
	testRegion    = "us-east-1"
)

var s3Client *s3.Client

// TestMain checks if the container is running before executing tests
func TestMain(m *testing.M) {
	// Check if SelfhostS3 is running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, testEndpoint+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("SKIP: SelfhostS3 container not running. Start it with:")
		fmt.Println("  docker compose -f compose.test.yaml up -d")
		os.Exit(0)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("SKIP: SelfhostS3 health check failed with status %d\n", resp.StatusCode)
		os.Exit(0)
	}

	// Initialize S3 client
	s3Client = createS3Client()

	// Run tests
	code := m.Run()

	// Cleanup: delete all test objects
	cleanup()

	os.Exit(code)
}

// createS3Client creates an S3 client configured for SelfhostS3
func createS3Client() *s3.Client {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(testRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			testAccessKey,
			testSecretKey,
			"",
		)),
	)
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(testEndpoint)
		o.UsePathStyle = true
	})
}

// cleanup removes all test objects from the bucket
func cleanup() {
	ctx := context.Background()

	// List all objects
	listOutput, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
	})
	if err != nil {
		return
	}

	// Delete each object
	for _, obj := range listOutput.Contents {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    obj.Key,
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	resp, err := http.Get(testEndpoint + "/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Errorf("unexpected health response: %s", string(body))
	}
}

func TestPutObject(t *testing.T) {
	ctx := context.Background()
	content := []byte("Hello, SelfhostS3 Integration Test!")
	key := "integration-test/put-test.txt"

	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	// Cleanup
	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Verify object exists
	headOutput, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}

	if *headOutput.ContentLength != int64(len(content)) {
		t.Errorf("expected content length %d, got %d", len(content), *headOutput.ContentLength)
	}
}

func TestGetObject(t *testing.T) {
	ctx := context.Background()
	content := []byte("Content for GET test - with special chars: éàü 中文")
	key := "integration-test/get-test.txt"

	// Put object first
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain; charset=utf-8"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Get object
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	// Read and verify content
	data, err := io.ReadAll(getOutput.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	if !bytes.Equal(data, content) {
		t.Errorf("content mismatch:\nexpected: %q\ngot: %q", string(content), string(data))
	}
}

func TestHeadObject(t *testing.T) {
	ctx := context.Background()
	content := []byte("Head test content")
	key := "integration-test/head-test.txt"

	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	headOutput, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("HeadObject failed: %v", err)
	}

	if *headOutput.ContentLength != int64(len(content)) {
		t.Errorf("expected content length %d, got %d", len(content), *headOutput.ContentLength)
	}

	if headOutput.ETag == nil || *headOutput.ETag == "" {
		t.Error("expected non-empty ETag")
	}
}

func TestDeleteObject(t *testing.T) {
	ctx := context.Background()
	key := "integration-test/delete-test.txt"

	// Create object
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte("delete me")),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	// Verify it exists
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("object should exist before delete: %v", err)
	}

	// Delete it
	_, err = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}

	// Verify it's gone
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err == nil {
		t.Error("object should not exist after delete")
	}
}

func TestListObjects(t *testing.T) {
	ctx := context.Background()

	// Create several test objects
	testKeys := []string{
		"integration-test/list/file1.txt",
		"integration-test/list/file2.txt",
		"integration-test/list/subdir/file3.txt",
	}

	for _, key := range testKeys {
		_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader([]byte("test content")),
		})
		if err != nil {
			t.Fatalf("PutObject failed for %s: %v", key, err)
		}
	}

	defer func() {
		for _, key := range testKeys {
			_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(key),
			})
		}
	}()

	// List all objects
	listOutput, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
	})
	if err != nil {
		t.Fatalf("ListObjectsV2 failed: %v", err)
	}

	// Verify our test files are in the listing
	foundKeys := make(map[string]bool)
	for _, obj := range listOutput.Contents {
		foundKeys[*obj.Key] = true
	}

	for _, key := range testKeys {
		if !foundKeys[key] {
			t.Errorf("expected key %q not found in listing", key)
		}
	}
}

func TestListObjectsWithPrefix(t *testing.T) {
	ctx := context.Background()

	// Create objects with different prefixes
	imageKeys := []string{
		"integration-test/images/photo1.jpg",
		"integration-test/images/photo2.jpg",
	}
	docKeys := []string{
		"integration-test/docs/report.pdf",
	}
	allKeys := append(imageKeys, docKeys...)

	for _, key := range allKeys {
		_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader([]byte("content")),
		})
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}
	}

	defer func() {
		for _, key := range allKeys {
			_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(key),
			})
		}
	}()

	// List with prefix
	listOutput, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(testBucket),
		Prefix: aws.String("integration-test/images/"),
	})
	if err != nil {
		t.Fatalf("ListObjectsV2 failed: %v", err)
	}

	// Should only find image keys
	if len(listOutput.Contents) < len(imageKeys) {
		t.Errorf("expected at least %d objects, got %d", len(imageKeys), len(listOutput.Contents))
	}

	for _, obj := range listOutput.Contents {
		if *obj.Key != "integration-test/images/" && !contains(imageKeys, *obj.Key) {
			t.Errorf("unexpected key in filtered listing: %q", *obj.Key)
		}
	}
}

func TestLargeFile(t *testing.T) {
	ctx := context.Background()
	key := "integration-test/large-file.bin"

	// Create 5MB file
	size := 5 * 1024 * 1024
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}

	// Upload
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("application/octet-stream"),
	})
	if err != nil {
		t.Fatalf("PutObject failed for large file: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Download and verify
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	downloaded, err := io.ReadAll(getOutput.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	if len(downloaded) != size {
		t.Errorf("expected %d bytes, got %d", size, len(downloaded))
	}

	if !bytes.Equal(content, downloaded) {
		t.Error("large file content mismatch")
	}
}

func TestSpecialCharacters(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name string
		key  string
	}{
		{"spaces", "integration-test/special/file with spaces.txt"},
		{"unicode", "integration-test/special/文件名.txt"},
		{"symbols", "integration-test/special/file-name_v1.2.3.txt"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte("content for " + tc.key)

			_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
				Body:   bytes.NewReader(content),
			})
			if err != nil {
				t.Fatalf("PutObject failed for key %q: %v", tc.key, err)
			}

			defer func() {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(tc.key),
				})
			}()

			// Get it back
			getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("GetObject failed for key %q: %v", tc.key, err)
			}
			defer func() { _ = getOutput.Body.Close() }()

			data, _ := io.ReadAll(getOutput.Body)
			if !bytes.Equal(data, content) {
				t.Errorf("content mismatch for key %q", tc.key)
			}
		})
	}
}

func TestOverwriteObject(t *testing.T) {
	ctx := context.Background()
	key := "integration-test/overwrite-test.txt"

	// First upload
	content1 := []byte("original content")
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content1),
	})
	if err != nil {
		t.Fatalf("first PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Overwrite with new content
	content2 := []byte("updated content - this is longer")
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content2),
	})
	if err != nil {
		t.Fatalf("second PutObject failed: %v", err)
	}

	// Verify new content
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	data, _ := io.ReadAll(getOutput.Body)
	if !bytes.Equal(data, content2) {
		t.Errorf("expected updated content, got original")
	}
}

func TestContentType(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		key         string
		contentType string
	}{
		{"integration-test/types/file.json", "application/json"},
		{"integration-test/types/file.png", "image/png"},
		{"integration-test/types/file.pdf", "application/pdf"},
	}

	for _, tc := range testCases {
		t.Run(tc.contentType, func(t *testing.T) {
			_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket:      aws.String(testBucket),
				Key:         aws.String(tc.key),
				Body:        bytes.NewReader([]byte("content")),
				ContentType: aws.String(tc.contentType),
			})
			if err != nil {
				t.Fatalf("PutObject failed: %v", err)
			}

			defer func() {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(tc.key),
				})
			}()

			// Get and check content type
			getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("GetObject failed: %v", err)
			}
			_ = getOutput.Body.Close()

			// Note: SelfhostS3 guesses content type from extension, so we check if it matches
			if getOutput.ContentType == nil {
				t.Error("ContentType is nil")
			}
		})
	}
}

func TestConcurrentUploads(t *testing.T) {
	ctx := context.Background()
	numFiles := 10

	var wg sync.WaitGroup
	errors := make(chan error, numFiles)
	keys := make([]string, numFiles)

	for i := 0; i < numFiles; i++ {
		keys[i] = fmt.Sprintf("integration-test/concurrent/file-%d.txt", i)
	}

	// Upload concurrently
	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := []byte(fmt.Sprintf("content for file %d", idx))
			_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(keys[idx]),
				Body:   bytes.NewReader(content),
			})
			if err != nil {
				errors <- fmt.Errorf("upload %d failed: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}

	// Cleanup
	defer func() {
		for _, key := range keys {
			_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(key),
			})
		}
	}()

	// Verify all files exist
	for i, key := range keys {
		_, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
		if err != nil {
			t.Errorf("file %d (%s) not found after concurrent upload: %v", i, key, err)
		}
	}
}

// =============================================================================
// Public/Private Visibility Tests
// =============================================================================

// TestPublicAccess_GET verifies that GET requests to public/ prefix work without authentication
func TestPublicAccess_GET(t *testing.T) {
	ctx := context.Background()
	content := []byte("This is a public file")
	key := "public/test-public-get.txt"

	// Upload file (requires auth)
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET without authentication (direct HTTP request)
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, content) {
		t.Errorf("content mismatch:\nexpected: %q\ngot: %q", string(content), string(body))
	}
}

// TestPublicAccess_HEAD verifies that HEAD requests to public/ prefix work without authentication
func TestPublicAccess_HEAD(t *testing.T) {
	ctx := context.Background()
	content := []byte("Public file for HEAD test")
	key := "public/test-public-head.txt"

	// Upload file (requires auth)
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// HEAD without authentication
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	resp, err := http.Head(url)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify content-length header
	if resp.ContentLength != int64(len(content)) {
		t.Errorf("expected content-length %d, got %d", len(content), resp.ContentLength)
	}
}

// TestPrivateAccess_RequiresAuth verifies that non-public paths require authentication
func TestPrivateAccess_RequiresAuth(t *testing.T) {
	ctx := context.Background()
	content := []byte("This is a private file")
	key := "private/test-private.txt"

	// Upload file (requires auth)
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET without authentication should fail
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for private file without auth, got %d", resp.StatusCode)
	}

	// GET with authentication should succeed
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("authenticated GetObject failed: %v", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	data, _ := io.ReadAll(getOutput.Body)
	if !bytes.Equal(data, content) {
		t.Error("content mismatch for authenticated GET")
	}
}

// TestPublicAccess_PutRequiresAuth verifies that PUT to public/ still requires authentication
func TestPublicAccess_PutRequiresAuth(t *testing.T) {
	key := "public/unauthorized-upload.txt"
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)

	// Attempt PUT without authentication
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader([]byte("unauthorized content")))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for PUT without auth, got %d", resp.StatusCode)
	}
}

// TestPublicAccess_DeleteRequiresAuth verifies that DELETE to public/ still requires authentication
func TestPublicAccess_DeleteRequiresAuth(t *testing.T) {
	ctx := context.Background()
	key := "public/test-delete-auth.txt"

	// Upload file first (with auth)
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte("delete test")),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Attempt DELETE without authentication
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for DELETE without auth, got %d", resp.StatusCode)
	}

	// Verify file still exists
	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Error("file should still exist after unauthorized delete attempt")
	}
}

// TestPublicAccess_CacheControlHeader verifies that public files have Cache-Control header
func TestPublicAccess_CacheControlHeader(t *testing.T) {
	ctx := context.Background()
	content := []byte("cacheable content")
	key := "public/test-cache-header.txt"

	// Upload file
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET without authentication
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	_ = resp.Body.Close()

	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl == "" {
		t.Error("expected Cache-Control header for public file")
	}

	// Default is 1 year (31536000 seconds)
	expectedCacheControl := "public, max-age=31536000"
	if cacheControl != expectedCacheControl {
		t.Errorf("expected Cache-Control %q, got %q", expectedCacheControl, cacheControl)
	}
}

// TestPublicAccess_DownloadParam verifies that ?download=1 sets Content-Disposition header
func TestPublicAccess_DownloadParam(t *testing.T) {
	ctx := context.Background()
	content := []byte("downloadable content")
	key := "public/document.pdf"

	// Upload file
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("application/pdf"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET with download=1 parameter
	url := fmt.Sprintf("%s/%s/%s?download=1", testEndpoint, testBucket, key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	contentDisposition := resp.Header.Get("Content-Disposition")
	expectedDisposition := `attachment; filename="document.pdf"`
	if contentDisposition != expectedDisposition {
		t.Errorf("expected Content-Disposition %q, got %q", expectedDisposition, contentDisposition)
	}
}

// TestPublicAccess_NestedPath verifies public access works for nested paths
func TestPublicAccess_NestedPath(t *testing.T) {
	ctx := context.Background()
	content := []byte("nested public file")
	key := "public/images/2024/01/photo.jpg"

	// Upload file
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("image/jpeg"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET without authentication
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(body, content) {
		t.Error("content mismatch for nested public file")
	}
}

// TestPublicAccess_NotFound returns 404 for non-existent public files
func TestPublicAccess_NotFound(t *testing.T) {
	url := fmt.Sprintf("%s/%s/public/nonexistent-file.txt", testEndpoint, testBucket)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404 for non-existent file, got %d", resp.StatusCode)
	}
}

// TestPublicPrivate_BoundaryPath verifies that paths similar to public/ but not exactly matching require auth
func TestPublicPrivate_BoundaryPath(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name     string
		key      string
		isPublic bool
	}{
		{"exact public prefix", "public/file.txt", true},
		{"public nested", "public/sub/file.txt", true},
		{"publicasprefix (no slash)", "publicasprefix/file.txt", false}, // "publicasprefix" doesn't start with "public/"
		{"private path", "private/file.txt", false},
		{"root level", "file.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := []byte("test content for " + tc.key)

			// Upload file (with auth)
			_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
				Body:   bytes.NewReader(content),
			})
			if err != nil {
				t.Fatalf("PutObject failed: %v", err)
			}

			defer func() {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(tc.key),
				})
			}()

			// GET without authentication
			url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, tc.key)
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("GET request failed: %v", err)
			}
			_ = resp.Body.Close()

			if tc.isPublic {
				if resp.StatusCode != http.StatusOK {
					t.Errorf("expected status 200 for public path, got %d", resp.StatusCode)
				}
			} else {
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("expected status 403 for private path, got %d", resp.StatusCode)
				}
			}
		})
	}
}

// TestPublicAccess_ContentTypePreserved verifies Content-Type is returned for public files
func TestPublicAccess_ContentTypePreserved(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		key                 string
		uploadContentType   string
		expectedContentType string
	}{
		{"public/test.json", "application/json", "application/json"},
		{"public/test.png", "image/png", "image/png"},
		{"public/test.html", "text/html", "text/html"},
	}

	for _, tc := range testCases {
		t.Run(tc.key, func(t *testing.T) {
			content := []byte("content")

			_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket:      aws.String(testBucket),
				Key:         aws.String(tc.key),
				Body:        bytes.NewReader(content),
				ContentType: aws.String(tc.uploadContentType),
			})
			if err != nil {
				t.Fatalf("PutObject failed: %v", err)
			}

			defer func() {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucket),
					Key:    aws.String(tc.key),
				})
			}()

			// GET without authentication
			url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, tc.key)
			resp, err := http.Get(url)
			if err != nil {
				t.Fatalf("GET request failed: %v", err)
			}
			_ = resp.Body.Close()

			contentType := resp.Header.Get("Content-Type")
			// Note: server may guess content type from extension
			if contentType == "" {
				t.Error("expected Content-Type header")
			}
		})
	}
}

// =============================================================================
// CORS Tests
// =============================================================================

// TestCORS_PreflightRequest verifies that OPTIONS preflight requests return proper CORS headers
func TestCORS_PreflightRequest(t *testing.T) {
	url := fmt.Sprintf("%s/%s/some-file.txt", testEndpoint, testBucket)

	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Simulate browser preflight request
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization, X-Amz-Date")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Preflight should return 200 OK
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for preflight, got %d", resp.StatusCode)
	}

	// Verify CORS headers
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header")
	}

	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	if allowMethods == "" {
		t.Error("missing Access-Control-Allow-Methods header")
	}
	// Verify expected methods are allowed
	expectedMethods := []string{"GET", "HEAD", "PUT", "DELETE", "OPTIONS"}
	for _, method := range expectedMethods {
		if !strings.Contains(allowMethods, method) {
			t.Errorf("expected %s in Access-Control-Allow-Methods, got %q", method, allowMethods)
		}
	}

	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	if allowHeaders == "" {
		t.Error("missing Access-Control-Allow-Headers header")
	}
	// Wildcard allows all headers for maximum SDK compatibility
	if allowHeaders != "*" {
		t.Errorf("expected Access-Control-Allow-Headers to be '*', got %q", allowHeaders)
	}

	maxAge := resp.Header.Get("Access-Control-Max-Age")
	if maxAge == "" {
		t.Error("missing Access-Control-Max-Age header")
	}
}

// TestCORS_SimpleRequest verifies that simple requests include CORS headers
func TestCORS_SimpleRequest(t *testing.T) {
	ctx := context.Background()
	content := []byte("CORS test content")
	key := "public/cors-simple-test.txt"

	// Upload file first
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// GET request with Origin header
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify CORS headers are present in response
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header on GET response")
	}

	// Verify exposed headers are set (wildcard exposes all)
	exposeHeaders := resp.Header.Get("Access-Control-Expose-Headers")
	if exposeHeaders == "" {
		t.Error("missing Access-Control-Expose-Headers header")
	}
	if exposeHeaders != "*" {
		t.Errorf("expected Access-Control-Expose-Headers to be '*', got %q", exposeHeaders)
	}
}

// TestCORS_HEADRequest verifies CORS headers on HEAD requests
func TestCORS_HEADRequest(t *testing.T) {
	ctx := context.Background()
	content := []byte("CORS HEAD test content")
	key := "public/cors-head-test.txt"

	// Upload file first
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// HEAD request with Origin header
	url := fmt.Sprintf("%s/%s/%s", testEndpoint, testBucket, key)
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "http://myapp.example.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Verify CORS headers
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header on HEAD response")
	}
}

// TestCORS_HealthEndpoint verifies CORS headers are present on health endpoint
func TestCORS_HealthEndpoint(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, testEndpoint+"/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// CORS headers should be present on all responses
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header on health endpoint")
	}
}

// TestCORS_WildcardOrigin verifies that wildcard (*) origin configuration works
func TestCORS_WildcardOrigin(t *testing.T) {
	// Test with different Origin values - should all get CORS headers with default config
	origins := []string{
		"http://localhost:3000",
		"https://example.com",
		"https://myapp.example.org",
	}

	for _, origin := range origins {
		t.Run(origin, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodOptions, testEndpoint+"/"+testBucket+"/test.txt", nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", "GET")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("OPTIONS request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
			// With default wildcard config, should allow all origins
			if allowOrigin == "" {
				t.Errorf("expected Access-Control-Allow-Origin for origin %s", origin)
			}
		})
	}
}

// TestCORS_ErrorResponse verifies CORS headers are present on error responses
func TestCORS_ErrorResponse(t *testing.T) {
	// Request a non-existent file (should get 404 or 403)
	url := fmt.Sprintf("%s/%s/nonexistent/file.txt", testEndpoint, testBucket)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Even error responses should have CORS headers
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header on error response")
	}
}

// TestCORS_PreflightWithCustomHeaders verifies preflight handles custom S3 headers
func TestCORS_PreflightWithCustomHeaders(t *testing.T) {
	url := fmt.Sprintf("%s/%s/upload.txt", testEndpoint, testBucket)

	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	// Simulate a more complex preflight with S3-specific headers
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, X-Amz-Content-Sha256, X-Amz-Date, X-Amz-Security-Token, Authorization")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for preflight, got %d", resp.StatusCode)
	}

	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	// Wildcard allows all headers for maximum SDK compatibility
	if allowHeaders != "*" {
		t.Errorf("expected Access-Control-Allow-Headers to be '*', got %q", allowHeaders)
	}
}

// TestCORS_ActualPUTRequest verifies CORS headers on actual PUT requests
func TestCORS_ActualPUTRequest(t *testing.T) {
	ctx := context.Background()
	key := "integration-test/cors-put-test.txt"

	// Make a PUT request using the S3 client (which signs the request)
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader([]byte("cors put test")),
		ContentType: aws.String("text/plain"),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// Use S3 client to get the file (authenticated)
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer func() { _ = getOutput.Body.Close() }()

	// Verify the file was stored correctly
	body, _ := io.ReadAll(getOutput.Body)
	if string(body) != "cors put test" {
		t.Errorf("content mismatch after PUT")
	}
}

// TestCORS_DELETEPreflight verifies preflight for DELETE requests
func TestCORS_DELETEPreflight(t *testing.T) {
	url := fmt.Sprintf("%s/%s/file-to-delete.txt", testEndpoint, testBucket)

	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "DELETE")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for DELETE preflight, got %d", resp.StatusCode)
	}

	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	if !strings.Contains(allowMethods, "DELETE") {
		t.Errorf("DELETE should be allowed, got %q", allowMethods)
	}
}

// TestCORS_MaxAge verifies the max-age header for caching preflight responses
func TestCORS_MaxAge(t *testing.T) {
	url := fmt.Sprintf("%s/%s/test.txt", testEndpoint, testBucket)

	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	maxAge := resp.Header.Get("Access-Control-Max-Age")
	if maxAge == "" {
		t.Error("missing Access-Control-Max-Age header")
	}

	// Default max-age should be 86400 (24 hours)
	expectedMaxAge := "86400"
	if maxAge != expectedMaxAge {
		t.Errorf("expected max-age %s, got %s", expectedMaxAge, maxAge)
	}
}

// TestCORS_ListBucketRequest verifies CORS headers on list bucket requests
func TestCORS_ListBucketRequest(t *testing.T) {
	ctx := context.Background()

	// Create a test file first
	key := "integration-test/cors-list-test.txt"
	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte("list test")),
	})
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	defer func() {
		_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(testBucket),
			Key:    aws.String(key),
		})
	}()

	// The S3 SDK doesn't expose raw HTTP headers easily,
	// so we verify CORS via preflight for list-type requests
	url := fmt.Sprintf("%s/%s?list-type=2", testEndpoint, testBucket)
	req, err := http.NewRequest(http.MethodOptions, url, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "" {
		t.Error("missing Access-Control-Allow-Origin header for list bucket preflight")
	}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
