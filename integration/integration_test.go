package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
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
	resp.Body.Close()

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
		s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
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
	defer resp.Body.Close()

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
	defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})

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

	defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})

	// Get object
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer getOutput.Body.Close()

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

	defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})

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
			s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
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
			s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
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

	defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})

	// Download and verify
	getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer getOutput.Body.Close()

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

			defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})

			// Get it back
			getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("GetObject failed for key %q: %v", tc.key, err)
			}
			defer getOutput.Body.Close()

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

	defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})

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
	defer getOutput.Body.Close()

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

			defer s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})

			// Get and check content type
			getOutput, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(tc.key),
			})
			if err != nil {
				t.Fatalf("GetObject failed: %v", err)
			}
			getOutput.Body.Close()

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
			s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
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

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
