package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// These tests sign requests with the AWS SDK for Go v2, an implementation independent of
// this package, so a bug shared by the signer and the validator cannot make them pass.

const (
	testAccessKey = "AKIAIOSFODNN7EXAMPLE"
	testSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	testRegion    = "us-east-1"
	testEndpoint  = "http://localhost:9000"
	testBucket    = "test-bucket"
)

func newTestS3Client(httpClient s3.HTTPClient) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(testEndpoint),
		Region:       testRegion,
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(testAccessKey, testSecretKey, ""),
		HTTPClient:   httpClient,
		Retryer:      aws.NopRetryer{},
	})
}

// toServerRequest turns a request built by the SDK into the request a server receives:
// parsed from its request target, with its headers and body.
func toServerRequest(t *testing.T, method, rawURL string, header http.Header, body []byte) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, rawURL, bytes.NewReader(body))
	for name, values := range header {
		for _, v := range values {
			r.Header.Add(name, v)
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	r.Host = parsed.Host
	return r
}

var errCaptured = errors.New("captured")

// captureClient records the signed request the SDK would send, and sends nothing.
type captureClient struct {
	req  *http.Request
	body []byte
}

func (c *captureClient) Do(r *http.Request) (*http.Response, error) {
	c.req = r
	if r.Body != nil {
		c.body, _ = io.ReadAll(r.Body)
	}
	return nil, errCaptured
}

// signedBySDK returns the request the SDK signs in its Authorization header for an operation.
func signedBySDK(t *testing.T, call func(ctx context.Context, client *s3.Client) error) *http.Request {
	t.Helper()
	capture := &captureClient{}
	if err := call(context.Background(), newTestS3Client(capture)); !errors.Is(err, errCaptured) {
		t.Fatalf("expected the captured request, got %v", err)
	}
	return toServerRequest(t, capture.req.Method, capture.req.URL.String(), capture.req.Header, capture.body)
}

func presignPut(t *testing.T, key, contentType string, expires time.Duration) string {
	t.Helper()
	presigner := s3.NewPresignClient(newTestS3Client(nil))
	input := &s3.PutObjectInput{Bucket: aws.String(testBucket), Key: aws.String(key)}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	req, err := presigner.PresignPutObject(context.Background(), input, s3.WithPresignExpires(expires))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	return req.URL
}

func putTo(t *testing.T, presignedURL, contentType string, body []byte) *http.Request {
	t.Helper()
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return toServerRequest(t, http.MethodPut, presignedURL, header, body)
}

func newTestValidator() *SignatureV4 {
	return NewSignatureV4(testAccessKey, testSecretKey, testRegion)
}

func TestPresigned_ValidPut(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	err := newTestValidator().ValidateRequest(putTo(t, presigned, "image/png", []byte("png bytes")))
	if err != nil {
		t.Fatalf("expected a presigned PUT to pass, got %v", err)
	}
}

func TestPresigned_ValidPutWithSignedContentLength(t *testing.T) {
	body := []byte("exactly these bytes")
	presigner := s3.NewPresignClient(newTestS3Client(nil))
	req, err := presigner.PresignPutObject(context.Background(), &s3.PutObjectInput{
		Bucket:        aws.String(testBucket),
		Key:           aws.String("docs/report.txt"),
		ContentType:   aws.String("text/plain"),
		ContentLength: aws.Int64(int64(len(body))),
	}, s3.WithPresignExpires(5*time.Minute))
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.Contains(req.URL, "content-length") {
		t.Fatalf("expected content-length among the signed headers, got %s", req.URL)
	}

	if err := newTestValidator().ValidateRequest(putTo(t, req.URL, "text/plain", body)); err != nil {
		t.Fatalf("expected a PUT of the signed length to pass, got %v", err)
	}
	if err := newTestValidator().ValidateRequest(putTo(t, req.URL, "text/plain", append(body, '!'))); err == nil {
		t.Fatal("expected a PUT of another length to be refused")
	}
}

func TestPresigned_KeysWithSpecialCharacters(t *testing.T) {
	keys := []string{
		"images/logo@2x.png",
		"images/capture-(1).png",
		"images/a+b.png",
		"images/it's!.png",
		"images/my photo.png",
		"images/café.png",
		"images/tilde~name.png",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			presigned := presignPut(t, key, "image/png", 5*time.Minute)
			if err := newTestValidator().ValidateRequest(putTo(t, presigned, "image/png", []byte("x"))); err != nil {
				t.Fatalf("expected %q to pass, got %v", key, err)
			}
		})
	}
}

func TestPresigned_ContentTypeChanged(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)
	// Older SDKs sign only "host"; this test means nothing unless the type is signed.
	if !strings.Contains(presigned, "X-Amz-SignedHeaders=content-type%3Bhost") {
		t.Fatalf("expected the presigner to sign content-type, got %s", presigned)
	}

	err := newTestValidator().ValidateRequest(putTo(t, presigned, "text/html", []byte("<script>")))
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected a signature mismatch for another Content-Type, got %v", err)
	}
}

func TestPresigned_KeyChanged(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)
	other := strings.Replace(presigned, "/images/logo.png", "/images/other.png", 1)

	err := newTestValidator().ValidateRequest(putTo(t, other, "image/png", []byte("x")))
	if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("expected a signature mismatch for another key, got %v", err)
	}
}

func TestPresigned_MethodChanged(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	r := toServerRequest(t, http.MethodDelete, presigned, http.Header{"Content-Type": {"image/png"}}, nil)
	if err := newTestValidator().ValidateRequest(r); err == nil {
		t.Fatal("expected a URL signed for PUT to be refused for DELETE")
	}
}

func TestPresigned_WrongSecret(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	other := NewSignatureV4(testAccessKey, "another-secret", testRegion)
	if err := other.ValidateRequest(putTo(t, presigned, "image/png", []byte("x"))); err == nil {
		t.Fatal("expected a URL signed with another secret to be refused")
	}
}

func TestPresigned_WrongAccessKey(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	other := NewSignatureV4("ANOTHERKEY", testSecretKey, testRegion)
	err := other.ValidateRequest(putTo(t, presigned, "image/png", []byte("x")))
	if err == nil || !strings.Contains(err.Error(), "invalid access key") {
		t.Fatalf("expected invalid access key, got %v", err)
	}
}

// withQuery rewrites one query parameter of a presigned URL, leaving the rest as signed.
func withQuery(t *testing.T, rawURL, name, value string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if value == "" {
		q.Del(name)
	} else {
		q.Set(name, value)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// withSigningTime moves the signing time of a presigned URL, in X-Amz-Date and in the date of
// X-Amz-Credential together, so that a shift across midnight UTC keeps them consistent.
func withSigningTime(t *testing.T, rawURL string, at time.Time) string {
	t.Helper()
	at = at.UTC()
	u := withQuery(t, rawURL, "X-Amz-Date", at.Format("20060102T150405Z"))
	credential := testAccessKey + "/" + at.Format("20060102") + "/" + testRegion + "/s3/aws4_request"
	return withQuery(t, u, "X-Amz-Credential", credential)
}

func TestPresigned_Expired(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)
	stale := withSigningTime(t, presigned, time.Now().Add(-6*time.Minute))

	err := newTestValidator().ValidateRequest(putTo(t, stale, "image/png", []byte("x")))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected an expired URL to be refused, got %v", err)
	}
}

func TestPresigned_DateInTheFuture(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)
	future := withSigningTime(t, presigned, time.Now().Add(time.Hour))

	err := newTestValidator().ValidateRequest(putTo(t, future, "image/png", []byte("x")))
	if err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("expected a date in the future to be refused, got %v", err)
	}
}

func TestPresigned_OtherRegion(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	other := NewSignatureV4(testAccessKey, testSecretKey, "eu-west-1")
	err := other.ValidateRequest(putTo(t, presigned, "image/png", []byte("x")))
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("expected an error naming the region, got %v", err)
	}
}

func TestPresigned_InvalidParameters(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)
	tomorrow := time.Now().UTC().Add(24 * time.Hour).Format("20060102")

	tests := []struct {
		name, param, value, wantErr string
	}{
		{"expires zero", "X-Amz-Expires", "0", "X-Amz-Expires"},
		{"expires negative", "X-Amz-Expires", "-5", "X-Amz-Expires"},
		{"expires not a number", "X-Amz-Expires", "soon", "X-Amz-Expires"},
		{"expires beyond seven days", "X-Amz-Expires", "604801", "X-Amz-Expires"},
		{"expires missing", "X-Amz-Expires", "", "X-Amz-Expires"},
		{"other algorithm", "X-Amz-Algorithm", "AWS4-HMAC-SHA1", "X-Amz-Algorithm"},
		{"host not signed", "X-Amz-SignedHeaders", "content-type", "host"},
		{"credential not for s3", "X-Amz-Credential", testAccessKey + "/" + time.Now().UTC().Format("20060102") + "/" + testRegion + "/ec2/aws4_request", "X-Amz-Credential"},
		{"credential malformed", "X-Amz-Credential", testAccessKey + "/" + testRegion, "X-Amz-Credential"},
		{"credential date differs", "X-Amz-Credential", testAccessKey + "/" + tomorrow + "/" + testRegion + "/s3/aws4_request", "date"},
		{"date malformed", "X-Amz-Date", "yesterday", "X-Amz-Date"},
		{"signature missing", "X-Amz-Signature", "", "X-Amz-Signature"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tampered := withQuery(t, presigned, tt.param, tt.value)
			err := newTestValidator().ValidateRequest(putTo(t, tampered, "image/png", []byte("x")))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected an error mentioning %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestPresigned_PayloadHashHeaderIgnored(t *testing.T) {
	presigned := presignPut(t, "images/logo.png", "image/png", 5*time.Minute)

	// A presigned URL always signs UNSIGNED-PAYLOAD; a hash header sent with it changes nothing.
	r := putTo(t, presigned, "image/png", []byte("x"))
	r.Header.Set("X-Amz-Content-Sha256", hashSHA256("x"))
	if err := newTestValidator().ValidateRequest(r); err != nil {
		t.Fatalf("expected the payload hash header to be ignored, got %v", err)
	}
}

func TestHeaderAuth_ListWithPrefixNeedingEncoding(t *testing.T) {
	prefixes := []string{"plain/", "my folder/", "a+b/", "tilde~/", "café/", "x(1)@y/"}
	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			r := signedBySDK(t, func(ctx context.Context, c *s3.Client) error {
				_, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(testBucket), Prefix: aws.String(prefix)})
				return err
			})
			if err := newTestValidator().ValidateRequest(r); err != nil {
				t.Fatalf("expected a listing of %q to pass, got %v", prefix, err)
			}
		})
	}
}

func TestHeaderAuth_PutKeyWithSpecialCharacters(t *testing.T) {
	keys := []string{"images/logo@2x.png", "images/capture-(1).png", "images/a+b.png", "images/my photo.png"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			r := signedBySDK(t, func(ctx context.Context, c *s3.Client) error {
				_, err := c.PutObject(ctx, &s3.PutObjectInput{
					Bucket: aws.String(testBucket), Key: aws.String(key), Body: bytes.NewReader([]byte("x")),
				})
				return err
			})
			if err := newTestValidator().ValidateRequest(r); err != nil {
				t.Fatalf("expected a PUT of %q to pass, got %v", key, err)
			}
		})
	}
}

func TestHeaderAuth_PathDecodedByAProxy(t *testing.T) {
	r := signedBySDK(t, func(ctx context.Context, c *s3.Client) error {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(testBucket), Key: aws.String("images/capture-(1)@2x.png"), Body: bytes.NewReader([]byte("x")),
		})
		return err
	})
	// A reverse proxy that decodes the path forwards "(", ")" and "@" unencoded.
	r.URL.RawPath = ""
	r.URL.Path = "/" + testBucket + "/images/capture-(1)@2x.png"

	if err := newTestValidator().ValidateRequest(r); err != nil {
		t.Fatalf("expected a decoded path to pass, got %v", err)
	}
}

func TestHeaderAuth_PathSignedAsSentWithLowercaseEscapes(t *testing.T) {
	// A client that signs the path exactly as it sends it, with lowercase escapes.
	req, err := http.NewRequest(http.MethodGet, testEndpoint+"/"+testBucket+"/caf%c3%a9.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.EscapedPath() != "/"+testBucket+"/caf%c3%a9.png" {
		t.Fatalf("expected the lowercase escapes to be kept, got %s", req.URL.EscapedPath())
	}
	creds := aws.Credentials{AccessKeyID: testAccessKey, SecretAccessKey: testSecretKey}
	err = v4.NewSigner(func(o *v4.SignerOptions) { o.DisableURIPathEscaping = true }).
		SignHTTP(context.Background(), creds, req, unsignedPayload, "s3", testRegion, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	r := toServerRequest(t, req.Method, req.URL.String(), req.Header, nil)
	if err := newTestValidator().ValidateRequest(r); err != nil {
		t.Fatalf("expected a path signed as sent to pass, got %v", err)
	}
}

func TestHeaderAuth_SignedHeaderWithRepeatedSpaces(t *testing.T) {
	r := signedBySDK(t, func(ctx context.Context, c *s3.Client) error {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{
			Bucket:   aws.String(testBucket),
			Key:      aws.String("notes.txt"),
			Body:     bytes.NewReader([]byte("x")),
			Metadata: map[string]string{"note": "two  spaces   here"},
		})
		return err
	})
	if !strings.Contains(r.Header.Get("Authorization"), "x-amz-meta-note") {
		t.Fatalf("expected x-amz-meta-note to be signed, got %s", r.Header.Get("Authorization"))
	}
	if err := newTestValidator().ValidateRequest(r); err != nil {
		t.Fatalf("expected inner spaces to be collapsed, got %v", err)
	}
}

func TestHeaderAuth_SignedHeaderWithSeveralValues(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, testEndpoint+"/"+testBucket+"/file.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("X-Amz-Meta-Tag", "first")
	req.Header.Add("X-Amz-Meta-Tag", "second")
	creds := aws.Credentials{AccessKeyID: testAccessKey, SecretAccessKey: testSecretKey}
	if err := v4.NewSigner().SignHTTP(context.Background(), creds, req, unsignedPayload, "s3", testRegion, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-meta-tag") {
		t.Fatalf("expected x-amz-meta-tag to be signed, got %s", req.Header.Get("Authorization"))
	}

	r := toServerRequest(t, req.Method, req.URL.String(), req.Header, nil)
	if err := newTestValidator().ValidateRequest(r); err != nil {
		t.Fatalf("expected both values to be signed, got %v", err)
	}
}
