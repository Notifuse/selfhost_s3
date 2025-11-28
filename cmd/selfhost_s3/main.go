package main

import (
	"log"
	"os"

	"github.com/Notifuse/selfhost_s3/internal/config"
	"github.com/Notifuse/selfhost_s3/internal/server"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Configuration error: %v", err)
		log.Println("Required environment variables:")
		log.Println("  S3_BUCKET      - S3 bucket name")
		log.Println("  S3_ACCESS_KEY  - Access key for authentication")
		log.Println("  S3_SECRET_KEY  - Secret key for authentication")
		log.Println("Optional environment variables:")
		log.Println("  S3_PORT         - Port to listen on (default: 9000)")
		log.Println("  S3_STORAGE_PATH - Local directory for storage (default: ./data)")
		log.Println("  S3_REGION       - AWS region (default: us-east-1)")
		log.Println("  S3_CORS_ORIGINS - Allowed CORS origins (default: *)")
		log.Println("  S3_MAX_FILE_SIZE - Maximum upload size (default: 100MB)")
		os.Exit(1)
	}

	// Create and start server
	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
