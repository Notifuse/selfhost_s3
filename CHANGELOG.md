# Changelog

All notable changes to this project will be documented in this file.

## [v1.4] - 2026-11-23

### Added

- Presigned URLs: a request signed in its query string, as the AWS SDK presigners produce them, is accepted. The server checks the access key, the credential scope, the signing date and the expiry (up to 7 days), and requires `host` among the signed headers. A URL signed for another region gets an error that names both regions.
- CI lint job (`gofmt` and golangci-lint). CI now takes its Go version from `go.mod`.

### Fixed

- Signatures of keys and prefixes containing `(`, `)`, `@`, `+`, `!` or `'`, and of query values containing a space, were refused with `signature mismatch`. Paths and query strings are now encoded as Signature Version 4 requires.
- Signed headers with several values, or with runs of inner spaces, were refused with `signature mismatch`. They are now canonicalized as Signature Version 4 requires.
- The `linux/arm64` image contained an `amd64` binary. The image is now cross-compiled for each platform.
- The body of a signed upload was not checked against its `X-Amz-Content-Sha256` hash, so a captured request could be resent with another body. A mismatch now gets `400 XAmzContentSHA256Mismatch`.
- An upload sent without a length (chunked) that went over `S3_MAX_FILE_SIZE` was stored cut off at the limit. It now gets `413 EntityTooLarge`.
- An upload was written straight over the existing file, so a failed or interrupted upload destroyed the previous content. Uploads now go to a temporary file, renamed into place once complete. Temporary files left by a stop are removed at startup.

### Changed

- The AWS SDK for Go v2 used by the tests is updated to `service/s3` v1.116.0.
- The README now documents presigned URLs, the actual CORS headers, the limitations of `ListObjectsV2` and how to reach selfhost_s3 from Notifuse.

## [v1.3] - 2025-11-29

### Added

- Folder marker support: `PUT` with trailing slash (e.g., `folder/`) creates directories
- `DELETE` support for folder markers
- Empty directories persist after deleting files inside (S3-like behavior)
- Makefile with `test-unit`, `test-integration-up`, `test-integration-down`, `build` commands
- Folder marker integration tests

### Fixed

- `ListObjects` prefix filter now correctly matches directories with trailing slashes

### Improved

- Storage test coverage increased to 80%

## [v1.2] - 2025-11-28

### Added

- Version information in `/health` endpoint response: `{"status":"ok","version":"v1.2"}`
- Version logged on server startup: `SelfhostS3 v1.2 starting on :9000`

## [v1.1] - 2025-11-28

### Fixed

- CORS headers now use wildcards (`*`) for `Access-Control-Allow-Headers` and `Access-Control-Expose-Headers` to support AWS SDK v3 and other S3 clients that send custom headers like `amz-sdk-invocation-id` and `amz-sdk-request`

### Added

- Comprehensive CORS integration tests covering:
  - Preflight (OPTIONS) requests
  - Simple GET/HEAD requests with Origin header
  - Wildcard origin support
  - Error response CORS headers
  - S3-specific header handling
  - DELETE preflight requests
  - Max-age caching verification

### Improved

- Test coverage increased to 86.3% overall
  - Server: 85.3%
  - Storage: 77.1%
  - Auth: 93.7%
  - Config: 100%

## [v1.0] - 2025-11-28

### Added

- Initial release of SelfhostS3
- S3-compatible object storage server
- AWS Signature V4 authentication
- Public file access with configurable prefix (default: `public/`)
- Cache-Control headers for public files
- Download parameter (`?download=1`) for Content-Disposition header
- CORS support with configurable origins
- Health check endpoint (`/health`)
- Docker support with multi-platform builds (amd64, arm64)
- GitHub Actions CI/CD workflows
- Comprehensive unit and integration tests

### Features

- **Storage Operations**: PUT, GET, HEAD, DELETE objects
- **Listing**: ListObjectsV2 with prefix filtering
- **Content Types**: Automatic MIME type detection from file extension
- **Security**: Path traversal protection, AWS Sig V4 authentication
- **Public Access**: Unauthenticated GET/HEAD for public prefix
- **Configuration**: Environment variable based configuration
