# selfhost_s3

A minimal S3-compatible object storage server written in Go that persists files to the local filesystem. Designed for development and self-hosted deployments where a full S3 service is overkill.

## Features

- S3-compatible API (AWS Signature V4 authentication)
- Local filesystem storage
- Single binary, no dependencies
- Docker support

## Supported S3 Operations

selfhost_s3 implements the minimum S3 API required by the Notifuse file manager:

| Operation       | Description                                      |
| --------------- | ------------------------------------------------ |
| `GetObject`     | Download/serve files (used for file URLs)        |
| `ListObjectsV2` | List all objects in the bucket                   |
| `PutObject`     | Upload files and create folders                  |
| `DeleteObject`  | Delete files and folders                         |
| `HeadObject`    | Check if file exists (optional, but recommended) |

## Configuration

selfhost_s3 is configured via environment variables:

| Variable           | Required | Default     | Description                            |
| ------------------ | -------- | ----------- | -------------------------------------- |
| `S3_BUCKET`        | Yes      | -           | S3 bucket name                         |
| `S3_ACCESS_KEY`    | Yes      | -           | Access key for authentication          |
| `S3_SECRET_KEY`    | Yes      | -           | Secret key for authentication          |
| `S3_PORT`          | No       | `9000`      | Port to listen on                      |
| `S3_STORAGE_PATH`  | No       | `./data`    | Local directory for file storage       |
| `S3_REGION`        | No       | `us-east-1` | AWS region (for signature validation)  |
| `S3_CORS_ORIGINS`  | No       | `*`         | Allowed CORS origins (comma-separated) |
| `S3_MAX_FILE_SIZE` | No       | `100MB`     | Maximum upload file size               |

## Quick Start

### Using Go

```bash
# Build
go build -o selfhost_s3 ./cmd/selfhost_s3

# Run
export S3_BUCKET=my-bucket
export S3_ACCESS_KEY=myaccesskey
export S3_SECRET_KEY=mysecretkey
./selfhost_s3
```

### Using Docker

```bash
docker run -d \
  -p 9000:9000 \
  -v $(pwd)/data:/data \
  -e S3_BUCKET=my-bucket \
  -e S3_ACCESS_KEY=myaccesskey \
  -e S3_SECRET_KEY=mysecretkey \
  notifuse/selfhost_s3
```

### Docker Compose

```yaml
services:
  selfhost_s3:
    image: notifuse/selfhost_s3
    ports:
      - '9000:9000'
    volumes:
      - ./s3-data:/data
    environment:
      S3_BUCKET: my-bucket
      S3_ACCESS_KEY: myaccesskey
      S3_SECRET_KEY: mysecretkey
```

## Connecting from Notifuse

In your workspace settings, configure the File Manager with:

- **Endpoint**: `http://localhost:9000` (or your selfhost_s3 URL)
- **Bucket**: Your `S3_BUCKET` value
- **Access Key**: Your `S3_ACCESS_KEY` value
- **Secret Key**: Your `S3_SECRET_KEY` value
- **Region**: `us-east-1` (default)

## Storage Structure

Files are stored on the local filesystem mirroring the S3 key structure:

```
data/
└── my-bucket/
    ├── documents/
    │   └── report.pdf
    └── images/
        └── logo.png
```

- **Files**: Stored at `{storage_path}/{bucket}/{key}`
- **Folders**: Represented as empty files with keys ending in `/`

## CORS

Since browser clients connect directly to selfhost_s3, CORS headers are automatically included:

```
Access-Control-Allow-Origin: <from S3_CORS_ORIGINS>
Access-Control-Allow-Methods: GET, HEAD, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, x-amz-*
Access-Control-Expose-Headers: ETag, Content-Length, Content-Type
```

For production, set `S3_CORS_ORIGINS` to your specific domain(s):

```bash
S3_CORS_ORIGINS=https://app.example.com,https://admin.example.com
```

## Health Check

selfhost_s3 exposes a health endpoint for container orchestration:

```
GET /health
```

Returns `200 OK` with `{"status": "ok"}` when the server is running.

## API Examples

### Get File (Download/View)

```bash
# Via AWS CLI
aws s3 cp s3://my-bucket/path/myfile.txt ./myfile.txt \
  --endpoint-url http://localhost:9000

# Direct URL (for browser/images)
curl http://localhost:9000/my-bucket/path/image.png
```

### List Objects

```bash
aws s3api list-objects-v2 \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket
```

### Upload File

```bash
aws s3 cp myfile.txt s3://my-bucket/path/myfile.txt \
  --endpoint-url http://localhost:9000
```

### Delete File

```bash
aws s3 rm s3://my-bucket/path/myfile.txt \
  --endpoint-url http://localhost:9000
```

## Limitations

- **No multipart uploads**: Files are uploaded in a single request
- **No presigned URLs**: Direct authentication required
- **No versioning**: Files are overwritten in place
- **No bucket operations**: Bucket must be pre-configured via env var
- **Single bucket**: One selfhost_s3 instance = one bucket

## Client Code

The Notifuse file manager client is located at:
`/console/src/components/file_manager`

## Development

```bash
# Run tests
go test ./...

# Run with hot reload
go run ./cmd/selfhost_s3
```

## Implementation Notes

- **Standard library only** - `net/http` is sufficient, no web framework needed
- **AWS Signature V4** - Validates signatures with proper URI encoding for special characters
- **File locking** - Uses `sync.RWMutex` for concurrent read/write safety
- **Content-Type** - Guessed from file extension using Go's `mime` package
- **ETag** - Generated from file modification time and size
- **Path traversal** - Keys are sanitized to prevent `../` attacks

## License

MIT
