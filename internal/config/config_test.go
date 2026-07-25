package config

import (
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("SEARXNG_URL", "")
	t.Setenv("SEARXNG_TIMEOUT", "")
	t.Setenv("SEARCH_DEFAULT_LIMIT", "")
	t.Setenv("FETCH_MAX_CHARS", "")
	t.Setenv("USER_AGENT", "")
	t.Setenv("SEARCH_MAX_BYTES", "")
	t.Setenv("WEB_READ_CONCURRENCY", "")
	t.Setenv("MAX_SEARCH_RETRIES", "")
	t.Setenv("LOG_LEVEL", "")

	cfg := LoadConfig()

	if cfg.SearxngURL != "http://localhost:8080" {
		t.Errorf("SearxngURL = %q; want %q", cfg.SearxngURL, "http://localhost:8080")
	}
	if cfg.SearxngTimeout != 10*time.Second {
		t.Errorf("SearxngTimeout = %v; want %v", cfg.SearxngTimeout, 10*time.Second)
	}
	if cfg.SearchDefaultLimit != 5 {
		t.Errorf("SearchDefaultLimit = %d; want 5", cfg.SearchDefaultLimit)
	}
	if cfg.FetchMaxChars != 6000 {
		t.Errorf("FetchMaxChars = %d; want 6000", cfg.FetchMaxChars)
	}
	if cfg.UserAgent == "" {
		t.Errorf("UserAgent should not be empty")
	}
	if cfg.SearchMaxBytes != 1<<20 {
		t.Errorf("SearchMaxBytes = %d; want %d", cfg.SearchMaxBytes, 1<<20)
	}
	if cfg.WebReadConcurrency != 8 {
		t.Errorf("WebReadConcurrency = %d; want 8", cfg.WebReadConcurrency)
	}
	if cfg.MaxSearchRetries != 2 {
		t.Errorf("MaxSearchRetries = %d; want 2", cfg.MaxSearchRetries)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q; want %q", cfg.LogLevel, "info")
	}
}

func TestLoadConfig_SearxngURL_TrimsTrailingSlash(t *testing.T) {
	t.Setenv("SEARXNG_URL", "https://search.example.com/")
	cfg := LoadConfig()
	if cfg.SearxngURL != "https://search.example.com" {
		t.Errorf("SearxngURL = %q; want trailing slash trimmed", cfg.SearxngURL)
	}
}

func TestLoadConfig_SearxngTimeout_Valid(t *testing.T) {
	t.Setenv("SEARXNG_TIMEOUT", "30s")
	cfg := LoadConfig()
	if cfg.SearxngTimeout != 30*time.Second {
		t.Errorf("SearxngTimeout = %v; want 30s", cfg.SearxngTimeout)
	}
}

func TestLoadConfig_SearxngTimeout_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("SEARXNG_TIMEOUT", "not-a-duration")
	cfg := LoadConfig()
	if cfg.SearxngTimeout != 10*time.Second {
		t.Errorf("SearxngTimeout = %v; want default 10s", cfg.SearxngTimeout)
	}
}

func TestLoadConfig_SearchDefaultLimit_Boundaries(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"0", 5},
		{"-1", 5},
		{"1", 1},
		{"5", 5},
		{"10", 10},
		{"11", 10},
		{"100", 10},
	}
	for _, tt := range tests {
		t.Run("limit_"+tt.env, func(t *testing.T) {
			t.Setenv("SEARCH_DEFAULT_LIMIT", tt.env)
			cfg := LoadConfig()
			if cfg.SearchDefaultLimit != tt.want {
				t.Errorf("SEARCH_DEFAULT_LIMIT=%q -> %d; want %d", tt.env, cfg.SearchDefaultLimit, tt.want)
			}
		})
	}
}

func TestLoadConfig_FetchMaxChars_TooSmallFallsBack(t *testing.T) {
	t.Setenv("FETCH_MAX_CHARS", "50")
	cfg := LoadConfig()
	if cfg.FetchMaxChars != 6000 {
		t.Errorf("FetchMaxChars = %d; want 6000 (fallback for <100)", cfg.FetchMaxChars)
	}
}

func TestLoadConfig_FetchMaxChars_Valid(t *testing.T) {
	t.Setenv("FETCH_MAX_CHARS", "5000")
	cfg := LoadConfig()
	if cfg.FetchMaxChars != 5000 {
		t.Errorf("FetchMaxChars = %d; want 5000", cfg.FetchMaxChars)
	}
}

func TestLoadConfig_UserAgent(t *testing.T) {
	t.Setenv("USER_AGENT", "TestAgent/2.0")
	cfg := LoadConfig()
	if cfg.UserAgent != "TestAgent/2.0" {
		t.Errorf("UserAgent = %q; want %q", cfg.UserAgent, "TestAgent/2.0")
	}
}

func TestLoadConfig_SearchMaxBytes_TooSmallFallsBack(t *testing.T) {
	t.Setenv("SEARCH_MAX_BYTES", "100")
	cfg := LoadConfig()
	if cfg.SearchMaxBytes != 1<<20 {
		t.Errorf("SearchMaxBytes = %d; want default 1MiB (fallback for <1024)", cfg.SearchMaxBytes)
	}
}

func TestLoadConfig_SearchMaxBytes_Valid(t *testing.T) {
	t.Setenv("SEARCH_MAX_BYTES", "4096")
	cfg := LoadConfig()
	if cfg.SearchMaxBytes != 4096 {
		t.Errorf("SearchMaxBytes = %d; want 4096", cfg.SearchMaxBytes)
	}
}

func TestLoadConfig_WebReadConcurrency_Boundaries(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"0", 8},
		{"-1", 8},
		{"1", 1},
		{"8", 8},
		{"64", 64},
		{"65", 64},
		{"1000", 64},
	}
	for _, tt := range tests {
		t.Run("conc_"+tt.env, func(t *testing.T) {
			t.Setenv("WEB_READ_CONCURRENCY", tt.env)
			cfg := LoadConfig()
			if cfg.WebReadConcurrency != tt.want {
				t.Errorf("WEB_READ_CONCURRENCY=%q -> %d; want %d", tt.env, cfg.WebReadConcurrency, tt.want)
			}
		})
	}
}

func TestLoadConfig_MaxSearchRetries_Boundaries(t *testing.T) {
	tests := []struct {
		env  string
		want int
	}{
		{"-1", 0},
		{"0", 0},
		{"2", 2},
		{"5", 5},
		{"6", 5},
		{"100", 5},
	}
	for _, tt := range tests {
		t.Run("retries_"+tt.env, func(t *testing.T) {
			t.Setenv("MAX_SEARCH_RETRIES", tt.env)
			cfg := LoadConfig()
			if cfg.MaxSearchRetries != tt.want {
				t.Errorf("MAX_SEARCH_RETRIES=%q -> %d; want %d", tt.env, cfg.MaxSearchRetries, tt.want)
			}
		})
	}
}

func TestLoadConfig_LogLevel_Lowercased(t *testing.T) {
	t.Setenv("LOG_LEVEL", "DEBUG")
	cfg := LoadConfig()
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q; want %q", cfg.LogLevel, "debug")
	}
}

func TestLoadConfig_LogLevel_MixedCase(t *testing.T) {
	t.Setenv("LOG_LEVEL", "WaRn")
	cfg := LoadConfig()
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q; want %q", cfg.LogLevel, "warn")
	}
}

func TestGetEnv_EmptyStringReturnsDefault(t *testing.T) {
	t.Setenv("SEARXNG_URL", "")
	if got := getEnv("SEARXNG_URL", "default-val"); got != "default-val" {
		t.Errorf("getEnv with empty string = %q; want %q", got, "default-val")
	}
}

func TestGetEnvInt_NonNumericReturnsDefault(t *testing.T) {
	t.Setenv("SEARCH_DEFAULT_LIMIT", "abc")
	if got := getEnvInt("SEARCH_DEFAULT_LIMIT", 42); got != 42 {
		t.Errorf("getEnvInt with non-numeric = %d; want 42", got)
	}
}

func TestGetEnvInt_EmptyStringReturnsDefault(t *testing.T) {
	t.Setenv("SEARCH_DEFAULT_LIMIT", "")
	if got := getEnvInt("SEARCH_DEFAULT_LIMIT", 42); got != 42 {
		t.Errorf("getEnvInt with empty string = %d; want 42", got)
	}
}
