package code_test

import (
	"context"
	"encoding/json"
	"hexlet-go-crawler/code/crawler"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>Test</body></html>`))
	}))
	defer server.Close()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	if len(report.Pages) == 0 {
		t.Error("Отчет не содержит страниц")
	}
	if report.Pages[0].Status != "ok" {
		t.Errorf("Ожидался статус ok, получен %s", report.Pages[0].Status)
	}
}

func TestInvalidURL(t *testing.T) {
	opts := code.Options{URL: ""}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	_, err := code.Analyze(ctx, opts)
	if err == nil {
		t.Error("Ожидалась ошибка при пустом URL")
	}
}

func TestBrokenLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			html := `<html><body>
				<a href="/working">Working</a>
				<a href="/broken">Broken</a>
				<a href="http://example.com">External</a>
				<a href="#anchor">Anchor</a>
			</body></html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/working":
			w.WriteHeader(http.StatusOK)
		case "/broken":
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Fatalf("Ожидалась 1 страница, получено %d", len(report.Pages))
	}

	page := report.Pages[0]
	foundBroken := false
	for _, link := range page.BrokenLinks {
		if strings.Contains(link.URL, "/broken") {
			foundBroken = true
			if link.StatusCode != 404 {
				t.Errorf("Ожидался статус 404, получен %d", link.StatusCode)
			}
		}
		if strings.Contains(link.URL, "/working") {
			t.Error("Рабочая ссылка попала в битые")
		}
	}
	if !foundBroken {
		t.Error("Битая ссылка не найдена")
	}
}

func TestSEO(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Title</title>
    <meta name="description" content="Test Description">
</head>
<body>
    <h1>Test H1</h1>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	if len(report.Pages) == 0 {
		t.Fatal("Отчет не содержит страниц")
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if !seo.HasTitle || seo.Title != "Test Title" {
		t.Errorf("Неправильный title: %v", seo.Title)
	}
	if !seo.HasDescription || seo.Description != "Test Description" {
		t.Errorf("Неправильный description: %v", seo.Description)
	}
	if !seo.HasH1 || seo.H1 != "Test H1" {
		t.Errorf("Неправильный h1: %v", seo.H1)
	}
}

func TestRateLimiter(t *testing.T) {
	tests := []struct {
		name    string
		delay   time.Duration
		rps     float64
		wantNil bool
	}{
		{"без ограничений", 0, 0, true},
		{"с задержкой", 100 * time.Millisecond, 0, false},
		{"с RPS", 0, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := code.NewRateLimiter(tt.delay, tt.rps)
			if (rl == nil) != tt.wantNil {
				t.Errorf("NewRateLimiter() = %v, wantNil %v", rl, tt.wantNil)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	base, _ := url.Parse("http://example.com/")
	
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr bool
	}{
		{"абсолютный URL", "http://test.com/page", "http://test.com/page", false},
		{"относительный URL", "/page", "http://example.com/page", false},
		{"пустой URL", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := code.NormalizeURL(tt.rawURL, base)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSameDomain(t *testing.T) {
	root, _ := url.Parse("http://example.com")
	
	tests := []struct {
		name string
		link string
		want bool
	}{
		{"один домен", "http://example.com/page", true},
		{"другой домен", "http://test.com/page", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link, _ := url.Parse(tt.link)
			if got := code.IsSameDomain(link, root); got != tt.want {
				t.Errorf("IsSameDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsHTMLContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"text/html", "text/html; charset=utf-8", true},
		{"text/plain", "text/plain", false},
		{"пустая строка", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code.IsHTMLContent(tt.contentType); got != tt.want {
				t.Errorf("IsHTMLContent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	_, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Logf("Получена ожидаемая ошибка: %v", err)
	}
}