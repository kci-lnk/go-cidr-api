package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", envOrDefault("RUN_MODE", "http"), "run mode: http | web")
	addr := flag.String("addr", strings.TrimSpace(os.Getenv("ADDR")), "http listen address")
	dataFile := flag.String("data-file", envOrDefault("DATA_FILE", "china_city_cidrs.compact.json"), "path to cidr data json")
	flag.Parse()

	store, err := LoadStore(*dataFile)
	if err != nil {
		log.Fatalf("load cidr data failed: %v", err)
	}

	app := NewApp(store)
	runMode := strings.ToLower(strings.TrimSpace(*mode))

	switch runMode {
	case "http", "web":
		listenAddr := resolveListenAddr(runMode, *addr)
		log.Printf("starting %s server on %s", runMode, listenAddr)
		if err := runHTTPServer(listenAddr, app.HTTPHandler()); err != nil {
			log.Fatalf("http server exited: %v", err)
		}
	default:
		log.Fatalf("unsupported mode %q, expected http | web", *mode)
	}
}

func runHTTPServer(addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return server.ListenAndServe()
}

func resolveListenAddr(mode, explicitAddr string) string {
	addr := strings.TrimSpace(explicitAddr)
	if addr != "" {
		return normalizeListenAddr(addr)
	}

	if port := normalizePort(os.Getenv("PORT")); port != "" {
		if mode == "web" {
			return "0.0.0.0:" + port
		}
		return ":" + port
	}

	if mode == "web" {
		return "0.0.0.0:9000"
	}

	return ":8080"
}

func normalizeListenAddr(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}

	if _, err := strconv.Atoi(value); err == nil {
		return ":" + value
	}

	return value
}

func normalizePort(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, ":"))
	if value == "" {
		return ""
	}

	if _, err := strconv.Atoi(value); err != nil {
		return ""
	}

	return value
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
