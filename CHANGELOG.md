# Changelog

All notable changes to this project will be documented in this file.

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
