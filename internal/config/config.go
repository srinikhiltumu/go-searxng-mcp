// Package config loads environment-based configuration for the SearXNG MCP server.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the configuration for the SearXNG MCP server.
type Config struct {
	SearxngURL         string
	SearxngTimeout     time.Duration
	SearchDefaultLimit int
	FetchMaxChars      int
	UserAgent          string
	// SearchMaxBytes caps the response body size accepted from the SearXNG
	// /search endpoint, preventing a runaway backend from exhausting memory.
	SearchMaxBytes int
	// WebReadConcurrency bounds the number of concurrent web_read fetches.
	WebReadConcurrency int
	// MaxSearchRetries is the number of retry attempts for transient search
	// failures (connection errors, 5xx). A value of 0 disables retries.
	MaxSearchRetries int
	// LogLevel controls the structured (slog) log level: debug, info, warn, error.
	LogLevel string
}

// LoadConfig initializes configuration from environment variables with sensible defaults.
func LoadConfig() *Config {
	url := strings.TrimRight(getEnv("SEARXNG_URL", "http://localhost:8080"), "/")
	timeoutStr := getEnv("SEARXNG_TIMEOUT", "10s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 10 * time.Second
	}

	limit := getEnvInt("SEARCH_DEFAULT_LIMIT", 5)
	if limit < 1 {
		limit = 5
	} else if limit > 10 {
		limit = 10
	}

	maxChars := getEnvInt("FETCH_MAX_CHARS", 6000)
	if maxChars < 100 {
		maxChars = 6000
	}

	userAgent := getEnv("USER_AGENT", "Mozilla/5.0 (compatible; SearXNG-MCP/1.0; +https://github.com/srinikhiltumu/go-searxng-mcp)")

	maxBytes := getEnvInt("SEARCH_MAX_BYTES", 1<<20) // 1 MiB
	if maxBytes < 1024 {
		maxBytes = 1 << 20
	}

	webReadConcurrency := getEnvInt("WEB_READ_CONCURRENCY", 8)
	if webReadConcurrency < 1 {
		webReadConcurrency = 8
	} else if webReadConcurrency > 64 {
		webReadConcurrency = 64
	}

	maxSearchRetries := getEnvInt("MAX_SEARCH_RETRIES", 2)
	if maxSearchRetries < 0 {
		maxSearchRetries = 0
	} else if maxSearchRetries > 5 {
		maxSearchRetries = 5
	}

	logLevel := strings.ToLower(getEnv("LOG_LEVEL", "info"))

	return &Config{
		SearxngURL:         url,
		SearxngTimeout:     timeout,
		SearchDefaultLimit: limit,
		FetchMaxChars:      maxChars,
		UserAgent:          userAgent,
		SearchMaxBytes:     maxBytes,
		WebReadConcurrency: webReadConcurrency,
		MaxSearchRetries:   maxSearchRetries,
		LogLevel:           logLevel,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
