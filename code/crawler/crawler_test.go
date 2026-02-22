package code_test

import (
	"context"
	"encoding/json"
	code "hexlet-go-crawler/code/crawler"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
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

func TestJSONFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			html := `<!DOCTYPE html>
<html>
<head>
    <title>Example title</title>
    <meta name="description" content="Example description">
</head>
<body>
    <h1>Example H1</h1>
    <a href="/missing">Missing Link</a>
    <img src="/static/logo.png" alt="Logo">
</body>
</html>`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		case "/static/logo.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "12345")
			w.WriteHeader(http.StatusOK)
			w.Write(make([]byte, 12345))
		}
	}))
	defer server.Close()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()
	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	if report.RootURL != server.URL {
		t.Errorf("root_url = %s, ожидался %s", report.RootURL, server.URL)
	}
	if report.Depth != 1 {
		t.Errorf("depth = %d, ожидался 1", report.Depth)
	}
	if len(report.Pages) != 1 {
		t.Fatalf("pages = %d, ожидался 1", len(report.Pages))
	}

	page := report.Pages[0]
	if page.URL != server.URL {
		t.Errorf("page.url = %s, ожидался %s", page.URL, server.URL)
	}
	if page.Depth != 0 {
		t.Errorf("page.depth = %d, ожидался 0", page.Depth)
	}
	if page.HTTPStatus != 200 {
		t.Errorf("page.http_status = %d, ожидался 200", page.HTTPStatus)
	}
	if page.Status != "ok" {
		t.Errorf("page.status = %s, ожидался ok", page.Status)
	}
	if page.Error != "" {
		t.Errorf("page.error = %s, ожидалась пустая строка", page.Error)
	}

	if page.SEO == nil {
		t.Fatal("page.seo отсутствует")
	}
	if !page.SEO.HasTitle {
		t.Error("seo.has_title должен быть true")
	}
	if page.SEO.Title != "Example title" {
		t.Errorf("seo.title = %s, ожидался Example title", page.SEO.Title)
	}
	if !page.SEO.HasDescription {
		t.Error("seo.has_description должен быть true")
	}
	if page.SEO.Description != "Example description" {
		t.Errorf("seo.description = %s, ожидался Example description", page.SEO.Description)
	}
	if !page.SEO.HasH1 {
		t.Error("seo.has_h1 должен быть true")
	}
	if page.SEO.H1 != "Example H1" {
		t.Errorf("seo.h1 = %s, ожидался Example H1", page.SEO.H1)
	}

	if len(page.BrokenLinks) != 1 {
		t.Fatalf("broken_links = %d, ожидался 1", len(page.BrokenLinks))
	}
	brokenLink := page.BrokenLinks[0]
	if !strings.Contains(brokenLink.URL, "/missing") {
		t.Errorf("broken_link.url = %s, ожидался URL с /missing", brokenLink.URL)
	}
	if brokenLink.StatusCode != 404 {
		t.Errorf("broken_link.status_code = %d, ожидался 404", brokenLink.StatusCode)
	}
	if brokenLink.Error == "" {
		t.Error("broken_link.error не должен быть пустым")
	}

	if len(page.Assets) != 1 {
		t.Fatalf("assets = %d, ожидался 1", len(page.Assets))
	}
	asset := page.Assets[0]
	if !strings.Contains(asset.URL, "/static/logo.png") {
		t.Errorf("asset.url = %s, ожидался URL с /static/logo.png", asset.URL)
	}
	if asset.Type != "image" {
		t.Errorf("asset.type = %s, ожидался image", asset.Type)
	}
	if asset.StatusCode != 200 {
		t.Errorf("asset.status_code = %d, ожидался 200", asset.StatusCode)
	}
	if asset.SizeBytes != 12345 {
		t.Errorf("asset.size_bytes = %d, ожидался 12345", asset.SizeBytes)
	}
	if asset.Error != "" {
		t.Errorf("asset.error = %s, ожидалась пустая строка", asset.Error)
	}
}

func TestJSONWithIndent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Test</title>
</head>
<body>
    <h1>Test</h1>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts1 := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		IndentJSON:  false,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	opts2 := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		IndentJSON:  true,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	ctx := context.Background()

	result1, err := code.Analyze(ctx, opts1)
	if err != nil {
		t.Fatalf("Analyze без отступов вернул ошибку: %v", err)
	}

	result2, err := code.Analyze(ctx, opts2)
	if err != nil {
		t.Fatalf("Analyze с отступами вернул ошибку: %v", err)
	}

	// Парсим оба результата в структуры для сравнения содержимого
	var report1, report2 code.Report
	err = json.Unmarshal(result1, &report1)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON без отступов: %v", err)
	}

	err = json.Unmarshal(result2, &report2)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON с отступами: %v", err)
	}

	// Сравниваем структуры, а не строки JSON
	if report1.RootURL != report2.RootURL {
		t.Errorf("RootURL отличается: %s vs %s", report1.RootURL, report2.RootURL)
	}
	if report1.Depth != report2.Depth {
		t.Errorf("Depth отличается: %d vs %d", report1.Depth, report2.Depth)
	}
	if len(report1.Pages) != len(report2.Pages) {
		t.Errorf("Количество страниц отличается: %d vs %d", len(report1.Pages), len(report2.Pages))
	}

	// Проверяем форматирование
	if len(result2) <= len(result1) {
		t.Error("JSON с отступами должен быть длиннее")
	}

	if !strings.Contains(string(result2), "\n") {
		t.Error("JSON с отступами должен содержать переводы строк")
	}
	if !strings.Contains(string(result2), "  ") {
		t.Error("JSON с отступами должен содержать пробелы для отступов")
	}
}

func TestJSONRequiredFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
</head>
<body>
</body>
</html>`
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

	ctx := context.Background()
	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report map[string]interface{}
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	requiredRootFields := []string{"root_url", "depth", "generated_at", "pages"}
	for _, field := range requiredRootFields {
		if _, exists := report[field]; !exists {
			t.Errorf("Отсутствует обязательное поле root: %s", field)
		}
	}

	pages, ok := report["pages"].([]interface{})
	if !ok || len(pages) == 0 {
		t.Fatal("Отсутствуют pages")
	}

	page, ok := pages[0].(map[string]interface{})
	if !ok {
		t.Fatal("Некорректный формат page")
	}

	requiredPageFields := []string{"url", "depth", "http_status", "status", "error", "broken_links", "seo", "assets", "discovered_at"}
	for _, field := range requiredPageFields {
		if _, exists := page[field]; !exists {
			t.Errorf("Отсутствует обязательное поле page: %s", field)
		}
	}

	seo, ok := page["seo"].(map[string]interface{})
	if !ok {
		t.Fatal("Некорректный формат seo")
	}

	requiredSEOFields := []string{"has_title", "title", "has_description", "description", "has_h1", "h1"}
	for _, field := range requiredSEOFields {
		if _, exists := seo[field]; !exists {
			t.Errorf("Отсутствует обязательное поле seo: %s", field)
		}
	}

	_, ok = page["assets"].([]interface{})
	if !ok {
		t.Fatal("Некорректный формат assets")
	}

	_, ok = page["broken_links"].([]interface{})
	if !ok {
		t.Fatal("Некорректный формат broken_links")
	}
}

func TestJSONEmptyStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
</head>
<body>
</body>
</html>`
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

	ctx := context.Background()
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

	page := report.Pages[0]

	if page.Error != "" {
		t.Errorf("page.error должен быть пустой строкой, получен: %s", page.Error)
	}

	if page.SEO == nil {
		t.Fatal("SEO отсутствует")
	}

	if page.SEO.Title != "" {
		t.Errorf("seo.title должен быть пустой строкой, получен: %s", page.SEO.Title)
	}
	if page.SEO.Description != "" {
		t.Errorf("seo.description должен быть пустой строкой, получен: %s", page.SEO.Description)
	}
	if page.SEO.H1 != "" {
		t.Errorf("seo.h1 должен быть пустой строкой, получен: %s", page.SEO.H1)
	}
}

func TestJSONTimeFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Test</title>
</head>
<body>
    <h1>Test</h1>
</body>
</html>`
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

	ctx := context.Background()
	result, err := code.Analyze(ctx, opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report map[string]interface{}
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Ошибка при разборе JSON: %v", err)
	}

	generatedAt, ok := report["generated_at"].(string)
	if !ok {
		t.Fatal("generated_at должен быть строкой")
	}

	_, err = time.Parse(time.RFC3339, generatedAt)
	if err != nil {
		t.Errorf("generated_at не в формате ISO8601: %s, ошибка: %v", generatedAt, err)
	}

	pages, ok := report["pages"].([]interface{})
	if !ok || len(pages) == 0 {
		t.Fatal("Отсутствуют pages")
	}

	page, ok := pages[0].(map[string]interface{})
	if !ok {
		t.Fatal("Некорректный формат page")
	}

	discoveredAt, ok := page["discovered_at"].(string)
	if !ok {
		t.Fatal("discovered_at должен быть строкой")
	}

	_, err = time.Parse(time.RFC3339, discoveredAt)
	if err != nil {
		t.Errorf("discovered_at не в формате ISO8601: %s, ошибка: %v", discoveredAt, err)
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
			if link.Error == "" {
				t.Error("Для битой ссылки должна быть ошибка")
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
			if asset.Error != "" {
				t.Errorf("style.css содержит ошибку: %s", asset.Error)
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
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

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

	mu.Lock()
	defer mu.Unlock()

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
		{"неподдерживаемая схема", "ftp://test.com", "", true},
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
		{"поддомен", "http://sub.example.com", false},
		{"nil URL", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var link *url.URL
			if tt.link != "" {
				link, _ = url.Parse(tt.link)
			}
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
		{"application/json", "application/json", false},
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
		{"image jpeg", "http://example.com/image.jpeg", "image"},
		{"image png", "http://example.com/image.png", "image"},
		{"image gif", "http://example.com/image.gif", "image"},
		{"image svg", "http://example.com/image.svg", "image"},
		{"image webp", "http://example.com/image.webp", "image"},
		{"image ico", "http://example.com/favicon.ico", "image"},
		{"image bmp", "http://example.com/image.bmp", "image"},
		{"script js", "http://example.com/script.js", "script"},
		{"script mjs", "http://example.com/script.mjs", "script"},
		{"style css", "http://example.com/style.css", "style"},
		{"other", "http://example.com/file.txt", "other"},
		{"other no extension", "http://example.com/file", "other"},
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
		{"относительный URL", "/image.jpg", true},
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
			<link rel="stylesheet" href="https://cdn.com/theme.css">
			<script src="/script.js"></script>
			<script src="https://cdn.com/lib.js"></script>
		</head>
		<body>
			<img src="/image1.jpg">
			<img src="/image2.png">
			<img src="https://cdn.com/logo.svg">
			<a href="#anchor">Anchor</a>
		</body>
	</html>`

	assets := code.ExtractAssetURLs(html)

	expected := 7
	if len(assets) != expected {
		t.Errorf("ExtractAssetURLs() вернул %d ассетов, ожидалось %d", len(assets), expected)
	}
}

func TestDecodeHTMLEntities(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"amp", "&amp;", "&"},
		{"lt", "&lt;", "<"},
		{"gt", "&gt;", ">"},
		{"quot", "&quot;", "\""},
		{"apos", "&#39;", "'"},
		{"nbsp", "&nbsp;", " "},
		{"copy", "&copy;", "©"},
		{"reg", "&reg;", "®"},
		{"trade", "&trade;", "™"},
		{"euro", "&euro;", "€"},
		{"pound", "&pound;", "£"},
		{"yen", "&yen;", "¥"},
		{"cent", "&cent;", "¢"},
		{"sect", "&sect;", "§"},
		{"deg", "&deg;", "°"},
		{"смешанный", "Test &amp; Title &copy; 2024", "Test & Title © 2024"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := code.DecodeHTMLEntities(tt.input); got != tt.want {
				t.Errorf("DecodeHTMLEntities() = %v, want %v", got, tt.want)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Concurrency: 1,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}

	_, err := code.Analyze(ctx, opts)
	if err == nil {
		select {
		case <-ctx.Done():
			t.Log("Контекст отменен, но Analyze не вернул ошибку")
		default:
			t.Error("Ожидалась ошибка при отмене контекста")
		}
	}
}
