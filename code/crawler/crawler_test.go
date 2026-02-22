package code_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	code "hexlet-go-crawler/code/crawler"
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

func TestAssetExtraction(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Page</title>
    <link rel="stylesheet" href="/style.css">
    <script src="/script.js"></script>
</head>
<body>
    <img src="/image.jpg" alt="Test">
    <img src="https://example.com/external.jpg">
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("body { color: red; }"))
		case "/script.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("console.log('test');"))
		case "/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "2048")
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 2048))
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

	if len(report.Pages) == 0 {
		t.Fatal("Отчет не содержит страниц")
	}

	assets := report.Pages[0].Assets
	if len(assets) == 0 {
		t.Fatal("Ассеты не найдены")
	}

	foundTypes := make(map[string]bool)
	for _, asset := range assets {
		foundTypes[asset.Type] = true
		switch asset.URL {
		case server.URL + "/style.css":
			if asset.Type != "style" {
				t.Errorf("style.css имеет тип %s, ожидался style", asset.Type)
			}
			if asset.SizeBytes != 1024 {
				t.Errorf("style.css размер %d, ожидался 1024", asset.SizeBytes)
			}
		case server.URL + "/script.js":
			if asset.Type != "script" {
				t.Errorf("script.js имеет тип %s, ожидался script", asset.Type)
			}
		case server.URL + "/image.jpg":
			if asset.Type != "image" {
				t.Errorf("image.jpg имеет тип %s, ожидался image", asset.Type)
			}
			if asset.SizeBytes != 2048 {
				t.Errorf("image.jpg размер %d, ожидался 2048", asset.SizeBytes)
			}
		}
	}

	if !foundTypes["style"] || !foundTypes["script"] || !foundTypes["image"] {
		t.Errorf("Найдены не все типы ассетов: %v", foundTypes)
	}
}

func TestAssetCache(t *testing.T) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		switch r.URL.Path {
		case "/":
			html := `<html>
				<head>
					<link rel="stylesheet" href="/style.css">
				</head>
				<body>
					<img src="/image.jpg">
					<a href="/page1">Page 1</a>
					<a href="/page2">Page 2</a>
				</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/page1":
			html := `<html>
				<head>
					<link rel="stylesheet" href="/style.css">
				</head>
				<body>
					<img src="/image.jpg">
					<a href="/">Home</a>
				</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/page2":
			html := `<html>
				<head>
					<link rel="stylesheet" href="/style.css">
				</head>
				<body>
					<img src="/image.jpg">
					<a href="/">Home</a>
				</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Content-Length", "512")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("body { color: red; }"))
		case "/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 1024))
		}
	}))
	defer server.Close()

	requestCount = 0

	opts := code.Options{
		URL:         server.URL,
		Depth:       2,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	expectedRequests := 5
	if requestCount != expectedRequests {
		t.Errorf("Количество запросов: %d, ожидалось %d", requestCount, expectedRequests)
	}
}

func TestAssetWithoutContentLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			html := `<html>
				<head>
					<link rel="stylesheet" href="/style.css">
				</head>
				<body>
					<img src="/image.jpg">
				</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("body { color: red; }"))
		case "/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 2048))
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

	assets := report.Pages[0].Assets
	if len(assets) == 0 {
		t.Fatal("Ассеты не найдены")
	}

	for _, asset := range assets {
		if asset.SizeBytes == 0 {
			t.Errorf("Ассет %s имеет нулевой размер, хотя должен быть определен из тела", asset.URL)
		}
		if asset.Error != "" {
			t.Errorf("Ассет %s содержит ошибку: %s", asset.URL, asset.Error)
		}
	}
}

func TestAssetWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			html := `<html>
				<head>
					<link rel="stylesheet" href="/style.css">
					<script src="/script.js"></script>
				</head>
				<body>
					<img src="/image.jpg">
					<img src="/broken.jpg">
				</body>
			</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/style.css":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("body { color: red; }"))
		case "/script.js":
			w.WriteHeader(http.StatusNotFound)
		case "/image.jpg":
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 1024))
		case "/broken.jpg":
			w.WriteHeader(http.StatusInternalServerError)
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

	assets := report.Pages[0].Assets
	if len(assets) == 0 {
		t.Fatal("Ассеты не найдены")
	}

	foundBroken := false
	for _, asset := range assets {
		if strings.Contains(asset.URL, "/broken.jpg") {
			foundBroken = true
			if asset.StatusCode != 500 {
				t.Errorf("Ожидался статус 500 для broken.jpg, получен %d", asset.StatusCode)
			}
			if asset.Error == "" {
				t.Error("Для битого ассета должна быть ошибка")
			}
		}
		if strings.Contains(asset.URL, "/script.js") {
			if asset.StatusCode != 404 {
				t.Errorf("Ожидался статус 404 для script.js, получен %d", asset.StatusCode)
			}
			if asset.Error == "" {
				t.Error("Для отсутствующего скрипта должна быть ошибка")
			}
		}
	}

	if !foundBroken {
		t.Error("Битый ассет не найден")
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

func TestGetAssetType(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"image jpg", "http://example.com/image.jpg", "image"},
		{"image png", "http://example.com/image.png", "image"},
		{"image gif", "http://example.com/image.gif", "image"},
		{"image svg", "http://example.com/image.svg", "image"},
		{"script js", "http://example.com/script.js", "script"},
		{"script mjs", "http://example.com/script.mjs", "script"},
		{"style css", "http://example.com/style.css", "style"},
		{"other", "http://example.com/file.txt", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code.GetAssetType(tt.url); got != tt.want {
				t.Errorf("GetAssetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldCheckAsset(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"пустой URL", "", false},
		{"якорь", "#anchor", false},
		{"http URL", "http://example.com/image.jpg", true},
		{"https URL", "https://example.com/image.jpg", true},
		{"ftp URL", "ftp://example.com/image.jpg", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code.ShouldCheckAsset(tt.url); got != tt.want {
				t.Errorf("ShouldCheckAsset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractAssetURLs(t *testing.T) {
	html := `<html>
		<head>
			<link rel="stylesheet" href="/style.css">
			<script src="/script.js"></script>
		</head>
		<body>
			<img src="/image1.jpg">
			<img src="/image2.png">
		</body>
	</html>`

	assets := code.ExtractAssetURLs(html)

	if len(assets) != 4 {
		t.Errorf("ExtractAssetURLs() вернул %d ассетов, ожидалось 4", len(assets))
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
