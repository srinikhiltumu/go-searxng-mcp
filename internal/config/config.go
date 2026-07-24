package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the configuration for the SearXNG MCP server.
type Config struct {
	SearxngURL          string
	SearxngTimeout      time.Duration
	SearchDefaultLimit  int
	FetchMaxChars       int
	UserAgent           string
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

	return &Config{
		SearxngURL:         url,
		SearxngTimeout:     timeout,
		SearchDefaultLimit: limit,
		FetchMaxChars:      maxChars,
		UserAgent:          userAgent,
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
