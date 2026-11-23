# selfhost_s3

[![Go](https://github.com/Notifuse/selfhost_s3/actions/workflows/go.yml/badge.svg)](https://github.com/Notifuse/selfhost_s3/actions/workflows/go.yml)
[![codecov](https://codecov.io/gh/Notifuse/selfhost_s3/branch/main/graph/badge.svg)](https://codecov.io/gh/Notifuse/selfhost_s3)
[![Docker](https://github.com/Notifuse/selfhost_s3/actions/workflows/docker.yml/badge.svg)](https://github.com/Notifuse/selfhost_s3/actions/workflows/docker.yml)
[![Docker Hub](https://img.shields.io/docker/v/notifuse/selfhost_s3?label=Docker%20Hub&logo=docker)](https://hub.docker.com/r/notifuse/selfhost_s3)
[![Docker pulls](https://img.shields.io/docker/pulls/notifuse/selfhost_s3?logo=docker)](https://hub.docker.com/r/notifuse/selfhost_s3)
[![Go version](https://img.shields.io/github/go-mod/go-version/Notifuse/selfhost_s3)](go.mod)
[![License: MIT](https://img.shields.io/github/license/Notifuse/selfhost_s3)](LICENSE)

A small S3-compatible object storage server written in Go. It stores files on the local filesystem and speaks enough of the S3 API for the AWS SDKs, the AWS CLI and the [Notifuse](https://github.com/Notifuse/notifuse) file manager.

It is meant for development, testing and self-hosted deployments where running MinIO or a cloud bucket would be more than you need. One instance serves one bucket from one directory.

## Contents

- [Features](#features)
- [Quality and testing](#quality-and-testing)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Connecting from Notifuse](#connecting-from-notifuse)
- [Supported S3 operations](#supported-s3-operations)
- [Presigned URLs](#presigned-urls)
- [Public access](#public-access)
- [CORS](#cors)
- [Storage layout](#storage-layout)
- [Health check](#health-check)
- [Examples with the AWS CLI](#examples-with-the-aws-cli)
- [Limitations](#limitations)
- [Security](#security)
- [Development](#development)
- [License](#license)

## Features

- AWS Signature Version 4, in the `Authorization` header or in the query string of a presigned URL
- Local filesystem storage, with the same layout as the S3 keys
- Uploads written to a temporary file and moved into place only when complete, so a failed or refused upload never replaces a file
- Upload bodies checked against the `X-Amz-Content-Sha256` hash a client signs
- Folder markers (`PUT` on a key ending in `/`), as the S3 consoles create them
- Optional public prefix served without authentication, with `Cache-Control` headers
- `?download=1` to serve a file as an attachment
- CORS headers for browser clients, with configurable origins
- A single static binary that uses only the Go standard library, and a non-root Docker image for `linux/amd64` and `linux/arm64`

## Quality and testing

Every push and pull request to `main` runs three CI jobs ([workflow](.github/workflows/go.yml)):

- **Lint:** `gofmt` and [golangci-lint](https://golangci-lint.run/).
- **Unit tests:** run with the race detector. Coverage goes to [Codecov](https://codecov.io/gh/Notifuse/selfhost_s3), which the badge above reports.
- **Integration tests:** the job builds the Docker image, starts it and exercises it with the official [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2). The suite covers uploads, downloads, listings, deletes, folder markers, public access, CORS, keys with special characters, concurrent uploads and presigned uploads.

The tests of presigned URLs, special characters and header canonicalization sign their requests with the AWS SDK for Go v2, an implementation independent of the validator, so a mistake shared by both sides cannot make them pass. Each refusal rule (expired URL, changed `Content-Type`, wrong key or region, malformed parameters) has its own test.

## Quick start

### Docker

```bash
docker run -d \
  --name selfhost_s3 \
  -p 9000:9000 \
  -v $(pwd)/data:/data \
  -e S3_BUCKET=my-bucket \
  -e S3_ACCESS_KEY=myaccesskey \
  -e S3_SECRET_KEY=mysecretkey \
  notifuse/selfhost_s3:latest
```

### Docker Compose

Create a `compose.yaml`:

```yaml
services:
  selfhost_s3:
    image: notifuse/selfhost_s3:latest
    container_name: selfhost_s3
    restart: unless-stopped
    ports:
      - "9000:9000"
    volumes:
      - ./s3-data:/data
    environment:
      S3_BUCKET: my-bucket
      S3_ACCESS_KEY: myaccesskey
      S3_SECRET_KEY: mysecretkey
```

Then run:

```bash
docker compose up -d
```

### Build from source

```bash
git clone https://github.com/Notifuse/selfhost_s3.git
cd selfhost_s3
make build

export S3_BUCKET=my-bucket
export S3_ACCESS_KEY=myaccesskey
export S3_SECRET_KEY=mysecretkey
./bin/selfhost_s3
```

### Docker Hub

Images are published at [hub.docker.com/r/notifuse/selfhost_s3](https://hub.docker.com/r/notifuse/selfhost_s3) for `linux/amd64` and `linux/arm64`. The `latest` tag follows the newest release, and each release also has its own tag, such as `v1.4`. The image runs as an unprivileged user (UID 1000) and has a built-in health check.

## Configuration

selfhost_s3 reads its settings from environment variables:

| Variable                  | Required | Default     | Description                                                 |
| ------------------------- | -------- | ----------- | ----------------------------------------------------------- |
| `S3_BUCKET`               | Yes      | -           | Bucket name                                                 |
| `S3_ACCESS_KEY`           | Yes      | -           | Access key clients sign with                                |
| `S3_SECRET_KEY`           | Yes      | -           | Secret key clients sign with                                |
| `S3_PORT`                 | No       | `9000`      | Port to listen on                                           |
| `S3_STORAGE_PATH`         | No       | `./data`    | Directory where files are stored (`/data` in the image)     |
| `S3_REGION`               | No       | `us-east-1` | Region clients must sign for                                |
| `S3_CORS_ORIGINS`         | No       | `*`         | Allowed CORS origins, comma-separated                       |
| `S3_MAX_FILE_SIZE`        | No       | `100MB`     | Largest accepted upload (`KB`, `MB` or `GB`)                |
| `S3_PUBLIC_PREFIX`        | No       | `public/`   | Key prefix served without authentication (empty disables it) |
| `S3_PUBLIC_CACHE_MAX_AGE` | No       | `31536000`  | `Cache-Control` max-age of public files, in seconds         |

## Connecting from Notifuse

In the Notifuse file manager, open the storage settings, choose **Other S3-Compatible** and fill in:

- **S3 endpoint:** the URL of selfhost_s3, such as `https://s3.example.com`
- **S3 region:** your `S3_REGION` (`us-east-1` by default)
- **S3 access key** and **S3 secret key:** your `S3_ACCESS_KEY` and `S3_SECRET_KEY`
- **S3 bucket:** your `S3_BUCKET`
- **Path-style access:** enabled. selfhost_s3 reads the bucket from the path, not from the host name.

Pick an endpoint that both the Notifuse server and your users' browsers can reach. `http://localhost:9000` only works while everything runs on one machine: inside the Notifuse container, `localhost` is the container itself.

## Supported S3 operations

| Operation       | Notes                                                                 |
| --------------- | --------------------------------------------------------------------- |
| `PutObject`     | Uploads a file, or creates a folder when the key ends in `/`          |
| `GetObject`     | Downloads a file; `?download=1` adds `Content-Disposition: attachment` |
| `HeadObject`    | Returns the size, `ETag`, `Last-Modified` and `Content-Type` of a file |
| `DeleteObject`  | Deletes a file, or an empty folder                                    |
| `ListObjectsV2` | Lists the bucket, optionally under a `prefix`                         |

Any of these requests can be signed in the `Authorization` header or as a presigned URL.

## Presigned URLs

A presigned URL carries its signature in the query string, so a browser or a script can upload or download one object without holding the secret key. selfhost_s3 accepts the URLs that the AWS SDKs produce.

It checks the access key, the credential scope (date, region, `s3` service), the signing date (no more than 15 minutes in the future) and the expiry, which can be up to 7 days. `host` must be among the signed headers. The payload of a presigned request is never signed, as on S3. Any signed header, such as `Content-Type`, must be sent with the same value, otherwise the request gets `403 AccessDenied`.

Example with the AWS SDK for Go v2, for a five-minute upload URL:

```go
presigner := s3.NewPresignClient(client)
req, err := presigner.PresignPutObject(ctx, &s3.PutObjectInput{
    Bucket:      aws.String("my-bucket"),
    Key:         aws.String("images/logo.png"),
    ContentType: aws.String("image/png"),
}, s3.WithPresignExpires(5*time.Minute))
```

The client then sends the file with no credentials:

```bash
curl -X PUT -H "Content-Type: image/png" --data-binary @logo.png "$PRESIGNED_URL"
```

## Public access

Files under the public prefix (`public/` by default) answer `GET` and `HEAD` without authentication. Uploads and deletes under that prefix still need a signature. The server creates the public directory on startup, and public files carry a `Cache-Control` header for browsers and CDNs.

```
http://localhost:9000/my-bucket/public/images/logo.png
http://localhost:9000/my-bucket/public/report.pdf?download=1
```

Some settings:

```bash
S3_PUBLIC_PREFIX=assets/       # serve assets/ instead of public/
S3_PUBLIC_PREFIX=              # disable public access
S3_PUBLIC_CACHE_MAX_AGE=3600   # cache public files for one hour
S3_PUBLIC_CACHE_MAX_AGE=0      # send no Cache-Control header
```

## CORS

Every response carries CORS headers, and preflight `OPTIONS` requests get `200` without authentication:

```
Access-Control-Allow-Origin: <origin, see below>
Access-Control-Allow-Methods: GET, HEAD, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: *
Access-Control-Expose-Headers: *
Access-Control-Max-Age: 86400
```

With `S3_CORS_ORIGINS=*` (the default), the allowed origin is `*`. With a list of origins, the server echoes the request's origin when it is in the list, and the first origin of the list otherwise. In production, list the origins of your apps:

```bash
S3_CORS_ORIGINS=https://app.example.com,https://admin.example.com
```

## Storage layout

Files are stored under `{S3_STORAGE_PATH}/{bucket}/{key}`, and folders are directories:

```
data/
└── my-bucket/
    ├── public/
    ├── documents/
    │   └── report.pdf
    └── images/
        └── logo.png
```

A folder stays after its last file is deleted, as in the S3 consoles, until you delete the folder marker itself. Keys that would resolve outside the bucket directory are refused.

## Health check

```
GET /health
```

Answers `200` with the running version, without authentication:

```json
{"status":"ok","version":"v1.4"}
```

## Examples with the AWS CLI

```bash
# Upload
aws s3 cp myfile.txt s3://my-bucket/path/myfile.txt --endpoint-url http://localhost:9000

# Download
aws s3 cp s3://my-bucket/path/myfile.txt ./myfile.txt --endpoint-url http://localhost:9000

# List
aws s3api list-objects-v2 --bucket my-bucket --endpoint-url http://localhost:9000

# Delete
aws s3 rm s3://my-bucket/path/myfile.txt --endpoint-url http://localhost:9000
```

## Limitations

- One instance serves one bucket, named in `S3_BUCKET`. There are no bucket operations.
- No multipart uploads: a file goes up in one request, up to `S3_MAX_FILE_SIZE`.
- `ListObjectsV2` returns every matching key in one response. It ignores `max-keys`, `delimiter` and continuation tokens.
- No `DeleteObjects` (batch delete), no versioning, no ACLs and no object metadata.
- The `Content-Type` of a file is derived from its extension when it is read. The type sent at upload is not stored.
- `ETag` is derived from the file's modification time and size, not from an MD5 of its content.

## Security

- Put a reverse proxy (Caddy, nginx, Traefik) in front of selfhost_s3 for TLS. A signature does not encrypt anything, and a request signed with `UNSIGNED-PAYLOAD` or through a presigned URL does not sign its body: without TLS, someone who captures it can resend it with another body until it expires.
- Use a long random secret key. A presigned URL reveals its signature to whoever holds it, and a short key can be guessed offline from one.
- Set `S3_CORS_ORIGINS` to your own origins.
- Anything under the public prefix can be read by anyone who knows its key.

## Development

```bash
make test-unit              # unit tests
make test-integration-up    # build the image, start it, run the integration tests
make test-integration-down  # stop the test container
make build                  # build bin/selfhost_s3
golangci-lint run ./...     # lint, as in CI
```

Releases are listed in [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
