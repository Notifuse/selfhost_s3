package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// presignPut returns a presigned PUT URL for key, made the way Notifuse makes them.
func presignPut(t *testing.T, key, contentType string, expires time.Duration) string {
	t.Helper()
	req, err := s3.NewPresignClient(s3Client).PresignPutObject(context.Background(), &s3.PutObjectInput{
		Bucket:      aws.String(testBucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expires))
	if err != nil {
		t.Fatalf("presign %q: %v", key, err)
	}
	return req.URL
}

// putPresigned sends body to a presigned URL as a browser would: a plain PUT with a
// Content-Type and no credentials.
func putPresigned(t *testing.T, url, contentType string, body []byte) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

func deleteKey(key string) {
	_, _ = s3Client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
}

func readObject(t *testing.T, key string) []byte {
	t.Helper()
	out, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject %q: %v", key, err)
	}
	defer func() { _ = out.Body.Close() }()
	data, _ := io.ReadAll(out.Body)
	return data
}

func TestPresignedPut_UploadsObject(t *testing.T) {
	keys := []string{
		"integration-test/presigned/logo.png",
		"integration-test/presigned/logo@2x.png",
		"integration-test/presigned/capture-(1).png",
		"integration-test/presigned/a+b.png",
		"integration-test/presigned/my photo.png",
		"integration-test/presigned/café.png",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			defer deleteKey(key)
			body := []byte("png bytes for " + key)

			status, resp := putPresigned(t, presignPut(t, key, "image/png", 5*time.Minute), "image/png", body)
			if status != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", status, resp)
			}

			// The Content-Type of a read comes from the extension, so only the bytes are checked.
			if data := readObject(t, key); !bytes.Equal(data, body) {
				t.Errorf("stored content differs: %q", data)
			}
		})
	}
}

func TestPresignedPut_OtherContentTypeRefused(t *testing.T) {
	key := "integration-test/presigned/typed.png"
	defer deleteKey(key)

	status, _ := putPresigned(t, presignPut(t, key, "image/png", 5*time.Minute), "text/html", []byte("<script></script>"))
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for another Content-Type, got %d", status)
	}

	_, err := s3Client.HeadObject(context.Background(), &s3.HeadObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
	})
	if err == nil {
		t.Fatal("expected nothing stored after a refused upload")
	}
}

func TestPresignedPut_ExpiredRefused(t *testing.T) {
	key := "integration-test/presigned/late.png"
	defer deleteKey(key)

	url := presignPut(t, key, "image/png", time.Second)
	time.Sleep(2 * time.Second)

	status, _ := putPresigned(t, url, "image/png", []byte("x"))
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for an expired URL, got %d", status)
	}
}

func TestListObjects_PrefixNeedingEncoding(t *testing.T) {
	ctx := context.Background()
	prefixes := []string{
		"integration-test/list/my folder/",
		"integration-test/list/a+b/",
		"integration-test/list/x(1)@y/",
	}
	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			key := prefix + "file.txt"
			if _, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: aws.String(testBucket),
				Key:    aws.String(key),
				Body:   bytes.NewReader([]byte("x")),
			}); err != nil {
				t.Fatalf("PutObject %q: %v", key, err)
			}
			defer deleteKey(key)

			out, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
				Bucket: aws.String(testBucket),
				Prefix: aws.String(prefix),
			})
			if err != nil {
				t.Fatalf("ListObjectsV2 with prefix %q: %v", prefix, err)
			}
			found := false
			for _, obj := range out.Contents {
				if aws.ToString(obj.Key) == key {
					found = true
				}
			}
			if !found {
				t.Errorf("expected %q in the listing of %q", key, prefix)
			}
		})
	}
}

func TestSignedPut_BodyMustMatchSignedHash(t *testing.T) {
	key := "integration-test/integrity/logo.png"
	defer deleteKey(key)
	if _, err := s3Client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(testBucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte("original")),
	}); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// A request signed for one body and sent with another, as in a replay.
	sum := sha256.Sum256([]byte("original"))
	payloadHash := hex.EncodeToString(sum[:])
	req, err := http.NewRequest(http.MethodPut, testEndpoint+"/"+testBucket+"/"+key, strings.NewReader("replaced"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	creds := aws.Credentials{AccessKeyID: testAccessKey, SecretAccessKey: testSecretKey}
	if err := v4.NewSigner().SignHTTP(context.Background(), creds, req, payloadHash, "s3", testRegion, time.Now()); err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "XAmzContentSHA256Mismatch") {
		t.Fatalf("expected 400 XAmzContentSHA256Mismatch, got %d: %s", resp.StatusCode, body)
	}

	if data := readObject(t, key); string(data) != "original" {
		t.Fatalf("expected the original content to survive, got %q", data)
	}
}
