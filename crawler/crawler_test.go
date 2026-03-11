package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// --- Тесты для вспомогательных функций ---

func TestNewRateLimiter(t *testing.T) {
	tests := []struct {
		name     string
		delay    time.Duration
		rps      float64
		wantNil  bool
		expected time.Duration
	}{
		{"nil when no params", 0, 0, true, 0},
		{"delay only", 100 * time.Millisecond, 0, false, 100 * time.Millisecond},
		{"rps only", 0, 10, false, 100 * time.Millisecond},
		{"both rps and delay - rps wins", 1 * time.Second, 10, false, 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.delay, tt.rps)
			if tt.wantNil {
				if rl != nil {
					t.Error("Expected nil, got rate limiter")
				}
				return
			}
			if rl == nil {
				t.Fatal("Expected rate limiter, got nil")
			}
			if rl.MinInterval != tt.expected {
				t.Errorf("Expected minInterval %v, got %v", tt.expected, rl.MinInterval)
			}
		})
	}
}

func TestRateLimiterWait(t *testing.T) {
	rl := NewRateLimiter(100*time.Millisecond, 0)
	if rl == nil {
		t.Fatal("Failed to create rate limiter")
	}

	start := time.Now()
	err := rl.Wait(context.Background())
	if err != nil {
		t.Errorf("First Wait failed: %v", err)
	}
	err = rl.Wait(context.Background())
	if err != nil {
		t.Errorf("Second Wait failed: %v", err)
	}

	duration := time.Since(start)
	if duration < 100*time.Millisecond {
		t.Errorf("Expected at least 100ms between waits, got %v", duration)
	}
}

func TestExtractSEO(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected SEO
	}{
		{
			name: "all seo elements",
			html: `<html><head><title>Test Title</title><meta name="description" content="Test Description"></head><body><h1>Test H1</h1></body></html>`,
			expected: SEO{
				HasTitle:       true,
				Title:          "Test Title",
				HasDescription: true,
				Description:    "Test Description",
				HasH1:          true,
			},
		},
		{
			name: "no seo elements",
			html: `<html><body><p>No SEO</p></body></html>`,
			expected: SEO{
				HasTitle:       false,
				Title:          "",
				HasDescription: false,
				Description:    "",
				HasH1:          false,
			},
		},
		{
			name: "title only",
			html: `<html><head><title>Only Title</title></head><body></body></html>`,
			expected: SEO{
				HasTitle:       true,
				Title:          "Only Title",
				HasDescription: false,
				Description:    "",
				HasH1:          false,
			},
		},
		{
			name: "trim whitespace",
			html: `<html><head><title>  Trimmed Title  </title><meta name="description" content="  Trimmed Description  "></head><body><h1>  Trimmed H1  </h1></body></html>`,
			expected: SEO{
				HasTitle:       true,
				Title:          "Trimmed Title",
				HasDescription: true,
				Description:    "Trimmed Description",
				HasH1:          true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seo := ExtractSEO(tt.html)
			if seo.HasTitle != tt.expected.HasTitle {
				t.Errorf("HasTitle: expected %v, got %v", tt.expected.HasTitle, seo.HasTitle)
			}
			if seo.Title != tt.expected.Title {
				t.Errorf("Title: expected %q, got %q", tt.expected.Title, seo.Title)
			}
			if seo.HasDescription != tt.expected.HasDescription {
				t.Errorf("HasDescription: expected %v, got %v", tt.expected.HasDescription, seo.HasDescription)
			}
			if seo.Description != tt.expected.Description {
				t.Errorf("Description: expected %q, got %q", tt.expected.Description, seo.Description)
			}
			if seo.HasH1 != tt.expected.HasH1 {
				t.Errorf("HasH1: expected %v, got %v", tt.expected.HasH1, seo.HasH1)
			}
		})
	}
}

func TestExtractLinks(t *testing.T) {
	html := `
		<html>
			<body>
				<a href="/page1">Page 1</a>
				<a href="https://example.com/page2">Page 2</a>
				<a href="#anchor">Anchor</a>
				<a href="mailto:test@example.com">Email</a>
			</body>
		</html>
	`

	links := ExtractLinks(html)
	expected := []string{"/page1", "https://example.com/page2", "#anchor", "mailto:test@example.com"}

	if len(links) != len(expected) {
		t.Errorf("Expected %d links, got %d", len(expected), len(links))
	}

	for i, link := range links {
		if i < len(expected) && link != expected[i] {
			t.Errorf("Expected link %q, got %q", expected[i], link)
		}
	}
}

func TestExtractAssetURLs(t *testing.T) {
	html := `
		<html>
			<body>
				<img src="/image.png">
				<script src="/script.js"></script>
				<link rel="stylesheet" href="/style.css">
				<img src="">
				<script></script>
			</body>
		</html>
	`

	assets := ExtractAssetURLs(html)
	expected := []string{"/image.png", "/script.js", "/style.css"}

	if len(assets) != len(expected) {
		t.Errorf("Expected %d assets, got %d", len(expected), len(assets))
	}

	for i, asset := range assets {
		if i < len(expected) && asset != expected[i] {
			t.Errorf("Expected asset %q, got %q", expected[i], asset)
		}
	}
}

func TestIsSameDomain(t *testing.T) {
	u1, _ := url.Parse("https://example.com/page1")
	u2, _ := url.Parse("https://example.com/page2")
	u3, _ := url.Parse("https://other.com/page")

	if !IsSameDomain(u1, u2) {
		t.Error("Expected same domain for example.com")
	}
	if IsSameDomain(u1, u3) {
		t.Error("Expected different domains for example.com and other.com")
	}
}

func TestShouldCheckAsset(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://example.com/image.png", true},
		{"http://example.com/script.js", true},
		{"ftp://example.com/file", false},
		{"", false},
		{"relative/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := ShouldCheckAsset(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

// --- Тесты с HTTP сервером ---

func TestGetHTMLWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("<html><body>Test</body></html>")); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	html, _, err := GetHTMLWithContext(context.Background(), server.URL, client, "TestBot/1.0")
	if err != nil {
		t.Fatalf("GetHTMLWithContext failed: %v", err)
	}
	if !strings.Contains(html, "Test") {
		t.Errorf("Expected HTML containing 'Test', got %q", html)
	}
}

func TestGetHTMLWithContextError(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	_, _, err := GetHTMLWithContext(context.Background(), "http://invalid.url.test", client, "")
	if err == nil {
		t.Error("Expected error for invalid URL, got nil")
	}
}

// --- Тесты для crawlPage ---

func TestCrawlPageSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
			<html>
				<head><title>Test Page</title></head>
				<body>
					<h1>Test H1</h1>
					<a href="/page1">Link</a>
					<img src="/image.png">
					<script src="/script.js"></script>
				</body>
			</html>
		`
		if _, err := w.Write([]byte(html)); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		if _, err := w.Write([]byte(strings.Repeat("x", 100))); err != nil {
			t.Fatal(err)
		}
	}))
	defer assetServer.Close()

	client := &http.Client{
		Transport: &testTransport{
			assetServer: assetServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		HTTPClient: client,
		UserAgent:  "TestBot/1.0",
	}
	html, _, err1 := GetHTMLWithContext(context.Background(), server.URL, opts.HTTPClient, opts.UserAgent)
	page, err := crawlPage(context.Background(), opts, server.URL, 0, html, 200, err1)
	if err != nil {
		t.Fatalf("crawlPage failed: %v", err)
	}
	if page.SEO == nil || !page.SEO.HasTitle {
		t.Error("Expected SEO with title")
	}
	if page.SEO.Title != "Test Page" {
		t.Errorf("Expected title 'Test Page', got %q", page.SEO.Title)
	}
	if len(page.Assets) != 2 {
		t.Errorf("Expected 2 assets, got %d", len(page.Assets))
	}
}

func TestCrawlPageError(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second}
	opts := Options{
		HTTPClient: client,
	}
	html, _, err1 := GetHTMLWithContext(context.Background(), "http://invalid.url.test", opts.HTTPClient, opts.UserAgent)
	page, err := crawlPage(context.Background(), opts, "http://invalid.url.test", 0, html, 200, err1)
	if err != nil {
		t.Fatalf("crawlPage should not return error, but got: %v", err)
	}

	if page.Status != "error" {
		t.Errorf("Expected status 'error', got %q", page.Status)
	}
	if page.Error == "" {
		t.Error("Expected non-empty error message")
	}
	if page.Assets != nil {
		t.Error("Expected Assets to be nil for error page")
	}
	if page.BrokenLinks != nil {
		t.Error("Expected BrokenLinks to be nil for error page")
	}
}

// --- Тесты для Analyze ---

func TestAnalyzeWithInvalidURL(t *testing.T) {
	opts := Options{
		URL:        "",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	_, err := Analyze(context.Background(), opts)
	if err == nil {
		t.Error("Expected error for empty URL, got nil")
	}
}

func TestAnalyzeSinglePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<html><head><title>Test</title></head><body>Test</body></html>`
		if _, err := w.Write([]byte(html)); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	opts := Options{
		URL:         server.URL,
		Depth:       0,
		HTTPClient:  client,
		Concurrency: 2,
	}

	data, err := Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Failed to unmarshal report: %v", err)
	}

	if report.RootURL != server.URL {
		t.Errorf("RootURL: expected %q, got %q", server.URL, report.RootURL)
	}
	if report.Depth != 0 {
		t.Errorf("Depth: expected 0, got %d", report.Depth)
	}
	if len(report.Pages) != 1 {
		t.Errorf("Expected 1 page, got %d", len(report.Pages))
	}
}
func TestAnalyzeWithAssets(t *testing.T) {
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
			<html>
				<head><title>Asset Test</title></head>
				<body>
					<img src="/image.png">
					<script src="/script.js"></script>
					<link rel="stylesheet" href="/style.css">
				</body>
			</html>
		`
		if _, err := w.Write([]byte(html)); err != nil {
			t.Fatal(err)
		}
	}))
	defer mainServer.Close()

	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			w.Header().Set("Content-Length", "1024")
		case "/script.js":
			w.Header().Set("Content-Length", "512")
		case "/style.css":
			w.Header().Set("Content-Length", "256")
		}
		if _, err := w.Write([]byte(strings.Repeat("x", 100))); err != nil {
			t.Fatal(err)
		}
	}))
	defer assetServer.Close()

	client := &http.Client{
		Transport: &testTransport{
			assetServer: assetServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         mainServer.URL,
		Depth:       0,
		HTTPClient:  client,
		Concurrency: 2,
	}

	data, err := Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Failed to unmarshal report: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Fatalf("Expected 1 page, got %d", len(report.Pages))
	}

	page := report.Pages[0]
	if len(page.Assets) != 3 {
		t.Errorf("Expected 3 assets, got %d", len(page.Assets))
	}
	if len(page.Assets) == 3 {
		if page.Assets[0].Type != "image" {
			t.Errorf("First asset should be image, got %s", page.Assets[0].Type)
		}
		if page.Assets[1].Type != "script" {
			t.Errorf("Second asset should be script, got %s", page.Assets[1].Type)
		}
		if page.Assets[2].Type != "style" {
			t.Errorf("Third asset should be style, got %s", page.Assets[2].Type)
		}
	}
}

func TestAnalyzeWithBrokenLinks(t *testing.T) {
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
			<html>
				<body>
					<a href="/working">Working</a>
					<a href="/broken">Broken</a>
				</body>
			</html>
		`
		if _, err := w.Write([]byte(html)); err != nil {
			t.Fatal(err)
		}
	}))
	defer mainServer.Close()

	linkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/working" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer linkServer.Close()

	client := &http.Client{
		Transport: &testTransport{
			linkServer: linkServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         mainServer.URL,
		Depth:       0,
		HTTPClient:  client,
		Concurrency: 2,
	}

	data, err := Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Failed to unmarshal report: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Fatalf("Expected 1 page, got %d", len(report.Pages))
	}

	page := report.Pages[0]
	if len(page.BrokenLinks) != 1 {
		t.Errorf("Expected 1 broken link, got %d", len(page.BrokenLinks))
	}

	if len(page.BrokenLinks) == 1 {
		if page.BrokenLinks[0].StatusCode != 404 {
			t.Errorf("Expected status 404, got %d", page.BrokenLinks[0].StatusCode)
		}
	}
}

func TestAnalyzeContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		if _, err := w.Write([]byte("<html><body>Slow</body></html>")); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	opts := Options{
		URL:         server.URL,
		Depth:       0,
		HTTPClient:  client,
		Concurrency: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	data, err := Analyze(ctx, opts)
	if err != nil {
		t.Logf("Got expected error: %v", err)
	} else {
		var report Report
		if err := json.Unmarshal(data, &report); err != nil {
			t.Errorf("Failed to unmarshal report: %v", err)
		}
	}
}

// --- Вспомогательные структуры для тестов ---

type testTransport struct {
	assetServer *httptest.Server
	linkServer  *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.assetServer != nil && strings.Contains(req.URL.Path, "/assets/") {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.assetServer.Listener.Addr().String()
		req.URL = &newURL
	}
	if t.linkServer != nil && (strings.Contains(req.URL.Path, "/working") || strings.Contains(req.URL.Path, "/broken")) {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.linkServer.Listener.Addr().String()
		req.URL = &newURL
	}
	return http.DefaultTransport.RoundTrip(req)
}

// --- Интеграционный тест с полным JSON отчетом ---

func TestAnalyzeJSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
			<!DOCTYPE html>
			<html>
				<head>
					<title>Test Page</title>
					<meta name="description" content="Test Description">
				</head>
				<body>
					<h1>Test Heading</h1>
					<a href="/about">About</a>
					<a href="/missing">Missing</a>
					<img src="/image.png">
				</body>
			</html>
		`
		if _, err := w.Write([]byte(html)); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()

	assetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1024")
		if _, err := w.Write([]byte(strings.Repeat("x", 1024))); err != nil {
			t.Fatal(err)
		}
	}))
	defer assetServer.Close()

	missingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missingServer.Close()

	client := &http.Client{
		Transport: &fullTestTransport{
			assetServer:   assetServer,
			missingServer: missingServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         server.URL,
		Depth:       0,
		HTTPClient:  client,
		Concurrency: 2,
		IndentJSON:  true,
	}

	data, err := Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("Failed to unmarshal report: %v", err)
	}

	if report.RootURL == "" {
		t.Error("RootURL is empty")
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero")
	}
	if len(report.Pages) == 0 {
		t.Error("No pages in report")
	}

	if !strings.Contains(string(data), "\n") {
		t.Error("IndentJSON=true should produce newlines")
	}
	if !strings.Contains(string(data), "  ") {
		t.Error("IndentJSON=true should produce indentation")
	}
}

type fullTestTransport struct {
	assetServer   *httptest.Server
	missingServer *httptest.Server
}

func (t *fullTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/image.png") {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.assetServer.Listener.Addr().String()
		req.URL = &newURL
	}
	if strings.Contains(req.URL.Path, "/missing") {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.missingServer.Listener.Addr().String()
		req.URL = &newURL
	}
	return http.DefaultTransport.RoundTrip(req)
}
