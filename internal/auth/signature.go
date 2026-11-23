package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// maxClockSkew is how far a request's X-Amz-Date may sit from the server's clock.
	maxClockSkew = 15 * time.Minute

	// maxPresignExpiry is the longest validity a presigned URL may claim: seven days, as on S3.
	maxPresignExpiry = 7 * 24 * 60 * 60

	// unsignedPayload is the payload hash of every presigned URL.
	unsignedPayload = "UNSIGNED-PAYLOAD"

	amzDateFormat = "20060102T150405Z"
)

// Credentials holds AWS-style credentials
type Credentials struct {
	AccessKey string
	SecretKey string
	Region    string
}

// SignatureV4 handles AWS Signature Version 4 authentication
type SignatureV4 struct {
	creds Credentials
}

// NewSignatureV4 creates a new signature validator
func NewSignatureV4(accessKey, secretKey, region string) *SignatureV4 {
	return &SignatureV4{
		creds: Credentials{
			AccessKey: accessKey,
			SecretKey: secretKey,
			Region:    region,
		},
	}
}

// authHeader represents parsed Authorization header
type authHeader struct {
	Algorithm     string
	Credential    string
	SignedHeaders []string
	Signature     string
	AccessKey     string
	Date          string
	Region        string
	Service       string
}

var authHeaderRegex = regexp.MustCompile(`AWS4-HMAC-SHA256\s+Credential=([^,]+),\s*SignedHeaders=([^,]+),\s*Signature=([a-f0-9]+)`)

// ValidateRequest validates an incoming HTTP request's AWS Signature V4, carried either in
// the Authorization header or, for a presigned URL, in the query string.
func (s *SignatureV4) ValidateRequest(r *http.Request) error {
	authHeaderValue := r.Header.Get("Authorization")
	if authHeaderValue == "" {
		if r.URL.Query().Has("X-Amz-Algorithm") {
			return s.validatePresignedRequest(r)
		}
		return fmt.Errorf("missing Authorization header")
	}

	auth, err := parseAuthHeader(authHeaderValue)
	if err != nil {
		return fmt.Errorf("invalid Authorization header: %w", err)
	}

	// Verify access key matches
	if auth.AccessKey != s.creds.AccessKey {
		return fmt.Errorf("invalid access key")
	}

	// Get the request date
	amzDate := r.Header.Get("X-Amz-Date")
	if amzDate == "" {
		return fmt.Errorf("missing X-Amz-Date header")
	}

	// Parse the date and check if it's within acceptable range (15 minutes)
	requestTime, err := time.Parse(amzDateFormat, amzDate)
	if err != nil {
		return fmt.Errorf("invalid X-Amz-Date format: %w", err)
	}

	timeDiff := time.Since(requestTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff > maxClockSkew {
		return fmt.Errorf("request timestamp too old or too far in future")
	}

	// Hashed payload
	hashedPayload := r.Header.Get("X-Amz-Content-Sha256")
	if hashedPayload == "" {
		hashedPayload = unsignedPayload
	}

	if !s.signatureMatches(r, auth.SignedHeaders, r.URL.Query(), hashedPayload, amzDate, auth.Signature) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// signatureMatches reports whether signature is the request's signature. It tries the
// canonical URI built the SigV4 way first and, when the client sent the path with another
// escaping (lowercase hex, an escaped unreserved character), the path as it was sent.
func (s *SignatureV4) signatureMatches(r *http.Request, signedHeaders []string, query url.Values, hashedPayload, amzDate, signature string) bool {
	canonicalURI := "/"
	if r.URL.Path != "" {
		canonicalURI = uriEncodePath(r.URL.Path)
	}
	candidates := []string{canonicalURI}
	if r.URL.RawPath != "" && r.URL.RawPath != canonicalURI {
		candidates = append(candidates, r.URL.RawPath)
	}

	for _, uri := range candidates {
		canonicalRequest := s.createCanonicalRequest(r, uri, signedHeaders, query, hashedPayload)
		if hmac.Equal([]byte(signature), []byte(s.calculateSignature(amzDate, canonicalRequest))) {
			return true
		}
	}
	return false
}

// validatePresignedRequest validates a request whose signature travels in the query string,
// as in a URL made by an S3 SDK's presigner. The payload of such a request is never signed.
func (s *SignatureV4) validatePresignedRequest(r *http.Request) error {
	query := r.URL.Query()

	if algorithm := query.Get("X-Amz-Algorithm"); algorithm != "AWS4-HMAC-SHA256" {
		return fmt.Errorf("unsupported X-Amz-Algorithm %q", algorithm)
	}

	// Credential: AccessKey/Date/Region/Service/aws4_request
	credParts := strings.Split(query.Get("X-Amz-Credential"), "/")
	if len(credParts) != 5 || credParts[3] != "s3" || credParts[4] != "aws4_request" {
		return fmt.Errorf("invalid X-Amz-Credential")
	}
	if credParts[0] != s.creds.AccessKey {
		return fmt.Errorf("invalid access key")
	}
	if credParts[2] != s.creds.Region {
		return fmt.Errorf("X-Amz-Credential region %q does not match the server region %q", credParts[2], s.creds.Region)
	}

	amzDate := query.Get("X-Amz-Date")
	signedAt, err := time.Parse(amzDateFormat, amzDate)
	if err != nil {
		return fmt.Errorf("invalid X-Amz-Date format: %w", err)
	}
	if credParts[1] != amzDate[:8] {
		return fmt.Errorf("X-Amz-Credential date does not match X-Amz-Date")
	}

	expires, err := strconv.Atoi(query.Get("X-Amz-Expires"))
	if err != nil || expires < 1 || expires > maxPresignExpiry {
		return fmt.Errorf("X-Amz-Expires must be a number of seconds between 1 and %d", maxPresignExpiry)
	}

	now := time.Now()
	if signedAt.After(now.Add(maxClockSkew)) {
		return fmt.Errorf("X-Amz-Date is in the future")
	}
	if now.After(signedAt.Add(time.Duration(expires) * time.Second)) {
		return fmt.Errorf("presigned URL has expired")
	}

	signedHeaders := strings.Split(query.Get("X-Amz-SignedHeaders"), ";")
	if !slices.Contains(signedHeaders, "host") {
		return fmt.Errorf("X-Amz-SignedHeaders must include host")
	}

	signature := query.Get("X-Amz-Signature")
	if signature == "" {
		return fmt.Errorf("missing X-Amz-Signature")
	}

	// The signature covers every query parameter but itself.
	query.Del("X-Amz-Signature")
	if !s.signatureMatches(r, signedHeaders, query, unsignedPayload, amzDate, signature) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// parseAuthHeader parses the AWS4-HMAC-SHA256 Authorization header
func parseAuthHeader(header string) (*authHeader, error) {
	matches := authHeaderRegex.FindStringSubmatch(header)
	if len(matches) != 4 {
		return nil, fmt.Errorf("malformed authorization header")
	}

	credential := matches[1]
	signedHeaders := strings.Split(matches[2], ";")
	signature := matches[3]

	// Parse credential: AccessKey/Date/Region/Service/aws4_request
	credParts := strings.Split(credential, "/")
	if len(credParts) != 5 {
		return nil, fmt.Errorf("invalid credential format")
	}

	return &authHeader{
		Algorithm:     "AWS4-HMAC-SHA256",
		Credential:    credential,
		SignedHeaders: signedHeaders,
		Signature:     signature,
		AccessKey:     credParts[0],
		Date:          credParts[1],
		Region:        credParts[2],
		Service:       credParts[3],
	}, nil
}

// calculateSignature computes the expected AWS Signature V4 of a canonical request
func (s *SignatureV4) calculateSignature(amzDate, canonicalRequest string) string {
	// Create string to sign
	dateStamp := amzDate[:8] // YYYYMMDD
	scope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, s.creds.Region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate,
		scope,
		hashSHA256(canonicalRequest),
	)

	// Calculate signature
	signingKey := s.deriveSigningKey(dateStamp)
	signature := hmacSHA256(signingKey, stringToSign)

	return hex.EncodeToString(signature)
}

// createCanonicalRequest creates the canonical request string
func (s *SignatureV4) createCanonicalRequest(r *http.Request, canonicalURI string, signedHeaders []string, query url.Values, hashedPayload string) string {
	// HTTP method
	method := r.Method

	// Canonical query string
	canonicalQueryString := s.createCanonicalQueryString(query)

	// Canonical headers
	canonicalHeaders := s.createCanonicalHeaders(r, signedHeaders)

	// Signed headers (lowercase, sorted, semicolon-separated)
	signedHeadersStr := strings.Join(signedHeaders, ";")

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeadersStr,
		hashedPayload,
	)
}

// createCanonicalQueryString creates the canonical query string
func (s *SignatureV4) createCanonicalQueryString(query url.Values) string {
	if len(query) == 0 {
		return ""
	}

	// Encode names and values, then sort by encoded name, then by encoded value
	type pair struct{ key, value string }
	pairs := make([]pair, 0, len(query))
	for k, values := range query {
		for _, v := range values {
			pairs = append(pairs, pair{sigV4Encode(k, true), sigV4Encode(v, true)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].key != pairs[j].key {
			return pairs[i].key < pairs[j].key
		}
		return pairs[i].value < pairs[j].value
	})

	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = p.key + "=" + p.value
	}
	return strings.Join(parts, "&")
}

// createCanonicalHeaders creates the canonical headers string
func (s *SignatureV4) createCanonicalHeaders(r *http.Request, signedHeaders []string) string {
	headers := make([]string, 0, len(signedHeaders))

	for _, h := range signedHeaders {
		var value string
		switch {
		case h == "host":
			value = r.Host
		case h == "content-length" && r.Header.Get(h) == "" && r.ContentLength >= 0:
			// net/http may keep the length only in ContentLength
			value = strconv.FormatInt(r.ContentLength, 10)
		default:
			// Several values are joined with commas, each trimmed, with its inner runs of
			// whitespace collapsed to one space.
			values := slices.Clone(r.Header.Values(h))
			for i, v := range values {
				values[i] = strings.Join(strings.Fields(v), " ")
			}
			value = strings.Join(values, ",")
		}
		value = strings.TrimSpace(value)
		headers = append(headers, fmt.Sprintf("%s:%s\n", h, value))
	}

	return strings.Join(headers, "")
}

// deriveSigningKey derives the signing key for AWS Signature V4
func (s *SignatureV4) deriveSigningKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+s.creds.SecretKey), dateStamp)
	kRegion := hmacSHA256(kDate, s.creds.Region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	return kSigning
}

// hmacSHA256 computes HMAC-SHA256
func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// hashSHA256 computes SHA256 hash and returns hex string
func hashSHA256(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

// uriEncodePath encodes a path according to AWS Signature V4 spec
// It encodes all characters except unreserved characters and forward slashes
func uriEncodePath(path string) string {
	return sigV4Encode(path, false)
}

// sigV4Encode percent-encodes every byte but the unreserved characters of RFC 3986, in
// uppercase hexadecimal, as AWS Signature V4 requires. Unlike url.PathEscape and
// url.QueryEscape, it encodes "(", ")", "@", "+", "!" and "'", and a space as %20.
// A slash is kept only when encodeSlash is false, for paths.
func sigV4Encode(s string, encodeSlash bool) string {
	const hex = "0123456789ABCDEF"
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) || (c == '/' && !encodeSlash) {
			buf.WriteByte(c)
			continue
		}
		buf.WriteByte('%')
		buf.WriteByte(hex[c>>4])
		buf.WriteByte(hex[c&0x0F])
	}
	return buf.String()
}

// isUnreserved returns true if the byte is an unreserved character per RFC 3986
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') ||
		(c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '~'
}
