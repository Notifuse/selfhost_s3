package server

import (
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Notifuse/selfhost_s3/internal/auth"
	"github.com/Notifuse/selfhost_s3/internal/config"
	"github.com/Notifuse/selfhost_s3/internal/storage"
)

// Server represents the SelfhostS3 HTTP server
type Server struct {
	config  *config.Config
	storage *storage.Storage
	auth    *auth.SignatureV4
}

// NewServer creates a new SelfhostS3 server
func NewServer(cfg *config.Config) (*Server, error) {
	store, err := storage.NewStorage(cfg.StoragePath, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Create public directory if configured
	if cfg.PublicPrefix != "" {
		if err := store.EnsurePublicDir(cfg.PublicPrefix); err != nil {
			return nil, fmt.Errorf("failed to create public directory: %w", err)
		}
		log.Printf("Public access enabled for prefix: %s", cfg.PublicPrefix)
	}

	return &Server{
		config:  cfg,
		storage: store,
		auth:    auth.NewSignatureV4(cfg.AccessKey, cfg.SecretKey, cfg.Region),
	}, nil
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Health check endpoint (no auth required)
	mux.HandleFunc("/health", s.handleHealth)

	// S3 API endpoints - all go through the main handler
	mux.HandleFunc("/", s.handleRequest)

	addr := fmt.Sprintf(":%d", s.config.Port)
	log.Printf("SelfhostS3 starting on %s", addr)
	log.Printf("Bucket: %s", s.config.Bucket)
	log.Printf("Storage path: %s", s.config.StoragePath)

	return http.ListenAndServe(addr, s.corsMiddleware(mux))
}

// corsMiddleware adds CORS headers to responses
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowedOrigin := "*"
		if len(s.config.CORSOrigins) > 0 && s.config.CORSOrigins[0] != "*" {
			for _, allowed := range s.config.CORSOrigins {
				if allowed == origin {
					allowedOrigin = origin
					break
				}
			}
			if allowedOrigin == "*" {
				allowedOrigin = s.config.CORSOrigins[0]
			}
		}

		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Access-Control-Expose-Headers", "*")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleRequest routes S3 API requests
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Log the request
	log.Printf("%s %s", r.Method, r.URL.Path)

	// Parse the path: /{bucket}/{key}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)

	// Validate bucket
	bucket := ""
	key := ""
	if len(parts) >= 1 {
		bucket = parts[0]
	}
	if len(parts) >= 2 {
		key = parts[1]
	}

	// Check if this is a public request (GET/HEAD on public prefix)
	isPublicRequest := s.config.PublicPrefix != "" &&
		strings.HasPrefix(key, s.config.PublicPrefix) &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead)

	// Validate authentication (skip for public requests)
	if !isPublicRequest {
		if err := s.auth.ValidateRequest(r); err != nil {
			log.Printf("Auth error: %v", err)
			s.sendError(w, http.StatusForbidden, "AccessDenied", err.Error())
			return
		}
	}

	// Check bucket matches configured bucket
	if bucket != s.config.Bucket {
		s.sendError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
		return
	}

	// Route based on method and query parameters
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Has("list-type") {
			s.handleListObjectsV2(w, r)
		} else if key == "" {
			// List objects (legacy)
			s.handleListObjectsV2(w, r)
		} else {
			s.handleGetObject(w, r, key, isPublicRequest)
		}
	case http.MethodHead:
		s.handleHeadObject(w, r, key, isPublicRequest)
	case http.MethodPut:
		s.handlePutObject(w, r, key)
	case http.MethodDelete:
		s.handleDeleteObject(w, r, key)
	default:
		s.sendError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed")
	}
}

// handleGetObject handles GET requests for objects
func (s *Server) handleGetObject(w http.ResponseWriter, r *http.Request, key string, isPublicRequest bool) {
	obj, reader, err := s.storage.GetObject(key)
	if err != nil {
		if err == storage.ErrNotFound {
			s.sendError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))

	// Add cache header for public files
	if isPublicRequest && s.config.PublicCacheMaxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", s.config.PublicCacheMaxAge))
	}

	// Handle download parameter
	if r.URL.Query().Get("download") == "1" {
		filename := filepath.Base(key)
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	w.WriteHeader(http.StatusOK)

	_, _ = io.Copy(w, reader)
}

// handleHeadObject handles HEAD requests for objects
func (s *Server) handleHeadObject(w http.ResponseWriter, r *http.Request, key string, isPublicRequest bool) {
	obj, err := s.storage.HeadObject(key)
	if err != nil {
		if err == storage.ErrNotFound {
			s.sendError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", obj.Size))
	w.Header().Set("ETag", obj.ETag)
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))

	// Add cache header for public files
	if isPublicRequest && s.config.PublicCacheMaxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", s.config.PublicCacheMaxAge))
	}

	w.WriteHeader(http.StatusOK)
}

// handlePutObject handles PUT requests to upload objects
func (s *Server) handlePutObject(w http.ResponseWriter, r *http.Request, key string) {
	// Check file size
	if r.ContentLength > s.config.MaxFileSize {
		s.sendError(w, http.StatusRequestEntityTooLarge, "EntityTooLarge",
			fmt.Sprintf("Your proposed upload exceeds the maximum allowed size of %d bytes", s.config.MaxFileSize))
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Limit reader to max file size
	limitedReader := io.LimitReader(r.Body, s.config.MaxFileSize+1)

	obj, err := s.storage.PutObject(key, contentType, limitedReader)
	if err != nil {
		if err == storage.ErrInvalidPath {
			s.sendError(w, http.StatusBadRequest, "InvalidArgument", "Invalid key")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.Header().Set("ETag", obj.ETag)
	w.WriteHeader(http.StatusOK)
}

// handleDeleteObject handles DELETE requests
func (s *Server) handleDeleteObject(w http.ResponseWriter, r *http.Request, key string) {
	err := s.storage.DeleteObject(key)
	if err != nil {
		if err == storage.ErrInvalidPath {
			s.sendError(w, http.StatusBadRequest, "InvalidArgument", "Invalid key")
			return
		}
		s.sendError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListObjectsV2 handles ListObjectsV2 requests
func (s *Server) handleListObjectsV2(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")

	objects, err := s.storage.ListObjects(prefix)
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Build response
	response := ListBucketResult{
		XMLName:     xml.Name{Local: "ListBucketResult"},
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        s.config.Bucket,
		Prefix:      prefix,
		KeyCount:    len(objects),
		MaxKeys:     1000,
		IsTruncated: false,
	}

	for _, obj := range objects {
		response.Contents = append(response.Contents, Contents{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified.UTC().Format(time.RFC3339),
			ETag:         obj.ETag,
			StorageClass: "STANDARD",
		})
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)

	xmlData, _ := xml.MarshalIndent(response, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(xmlData)
}

// sendError sends an S3-style error response
func (s *Server) sendError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(statusCode)

	errorResponse := ErrorResponse{
		XMLName: xml.Name{Local: "Error"},
		Code:    code,
		Message: message,
	}

	xmlData, _ := xml.MarshalIndent(errorResponse, "", "  ")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(xmlData)
}

// XML response structures

// ListBucketResult is the response for ListObjectsV2
type ListBucketResult struct {
	XMLName               xml.Name   `xml:"ListBucketResult"`
	Xmlns                 string     `xml:"xmlns,attr"`
	Name                  string     `xml:"Name"`
	Prefix                string     `xml:"Prefix"`
	KeyCount              int        `xml:"KeyCount"`
	MaxKeys               int        `xml:"MaxKeys"`
	IsTruncated           bool       `xml:"IsTruncated"`
	Contents              []Contents `xml:"Contents"`
	ContinuationToken     string     `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string     `xml:"NextContinuationToken,omitempty"`
}

// Contents represents an object in the list response
type Contents struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

// ErrorResponse is an S3 error response
type ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}
