package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

const (
	encodingBrotli = "br"
	encodingGzip   = "gzip"
)

func compressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		addVaryHeader(w.Header(), "Accept-Encoding")

		encoding := preferredEncoding(r.Header.Get("Accept-Encoding"))
		if encoding == "" {
			next.ServeHTTP(w, r)
			return
		}

		compressedWriter := newCompressedResponseWriter(w, encoding)
		defer compressedWriter.Close()

		next.ServeHTTP(compressedWriter, r)
	})
}

type compressedResponseWriter struct {
	http.ResponseWriter
	writer io.WriteCloser
}

func newCompressedResponseWriter(w http.ResponseWriter, encoding string) *compressedResponseWriter {
	header := w.Header()
	header.Del("Content-Length")
	header.Set("Content-Encoding", encoding)

	var writer io.WriteCloser
	switch encoding {
	case encodingBrotli:
		writer = brotli.NewWriter(w)
	default:
		writer = gzip.NewWriter(w)
	}

	return &compressedResponseWriter{
		ResponseWriter: w,
		writer:         writer,
	}
}

func (w *compressedResponseWriter) WriteHeader(statusCode int) {
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *compressedResponseWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *compressedResponseWriter) Flush() {
	if flusher, ok := w.writer.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *compressedResponseWriter) Close() error {
	return w.writer.Close()
}

func preferredEncoding(acceptEncoding string) string {
	bestEncoding := ""
	bestWeight := 0.0

	for _, part := range strings.Split(acceptEncoding, ",") {
		encoding, weight := parseEncoding(part)
		if weight <= 0 {
			continue
		}

		switch encoding {
		case encodingBrotli, encodingGzip:
		default:
			continue
		}

		if weight > bestWeight || (weight == bestWeight && encoding == encodingBrotli) {
			bestEncoding = encoding
			bestWeight = weight
		}
	}

	return bestEncoding
}

func parseEncoding(value string) (string, float64) {
	parts := strings.Split(strings.TrimSpace(value), ";")
	if len(parts) == 0 {
		return "", 0
	}

	encoding := strings.ToLower(strings.TrimSpace(parts[0]))
	if encoding == "" {
		return "", 0
	}

	weight := 1.0
	for _, param := range parts[1:] {
		param = strings.TrimSpace(param)
		if !strings.HasPrefix(strings.ToLower(param), "q=") {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(param, "q="))
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return encoding, 0
		}
		weight = parsed
		break
	}

	return encoding, weight
}

func addVaryHeader(header http.Header, value string) {
	existing := header.Values("Vary")
	for _, current := range existing {
		for _, item := range strings.Split(current, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
