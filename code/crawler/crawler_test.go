package code_test

import (
	"context"
	"encoding/json"
	code "hexlet-go-crawler/code/crawler"
	"testing"
	"net/http"
	"net/http/httptest"
	"time"
	"strings"
)

func TestSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := "https://example.com"
	test := code.Options{URL: url}
	res, err := code.Analyze(ctx, test)
	if err != nil {
		t.Errorf("Ошибка в функции %s", err)
	}
	testreport := &code.Report{}
	err = json.Unmarshal(res, testreport)
	if err != nil {
		t.Errorf("Ошибка в unmarshal %s", err)
	}
	if testreport.Pages[0].Status != "ok" {
		t.Error("Непрвильный статус")
	}
}
func TestAnalyze_InvalidURL(t *testing.T) {
	opts := code.Options{
		URL: "",
	}

	ctx := context.Background()
	_, err := code.Analyze(ctx, opts)
	if err == nil {
		t.Error("Ожидалась ошибка при пустом URL")
	}
}

func TestAnalyze_NetworkError(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    url := "http://nonexistent-domain-12345.com"
    opts := code.Options{
        URL:     url,
        Timeout: 1 * time.Second, 
    }
    res, err := code.Analyze(ctx, opts)
    if err == nil {
        t.Error("Ожидалась ошибка при запросе к несуществующему хосту")
    }
    if res != nil {
        t.Error("При ошибке результат должен быть nil")
    }
}

func TestAnalyze_BrokenLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			html := `
			<html>
				<body>
					<a href="/working">Working Link</a>
					<a href="/broken">Broken Link</a>
					<a href="http://example.com">External Link</a>
					<a href="ftp://unsupported.com">Unsupported Scheme</a>
					<a href="#anchor">Anchor Link</a>
					<a href="">Empty Link</a>
				</body>
			</html>
			`
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
			
		case "/working":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			
		case "/broken":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		}
	}))
	defer server.Close()
	opts := code.Options{
		URL:         server.URL,
		Depth:       1,
		Timeout:     5 * time.Second,
		UserAgent:   "TestBot/1.0",
		IndentJSON:  true,
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
	}
	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}
	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}
	if len(report.Pages) != 1 {
		t.Errorf("Ожидалась 1 страница, получено %d", len(report.Pages))
	}
	page := report.Pages[0]
	if len(page.BrokenLinks) == 0 {
		t.Error("Ожидались битые ссылки, но ничего не найдено")
	}
	foundBroken := false
	for _, link := range page.BrokenLinks {
		if strings.Contains(link.URL, "/broken") {
			foundBroken = true
			if link.StatusCode != http.StatusNotFound {
				t.Errorf("Ожидался статус 404 для /broken, получен %d", link.StatusCode)
			}
			break
		}
	}
	if !foundBroken {
		t.Error("Ссылка /broken не была отмечена как битая")
	}
	for _, link := range page.BrokenLinks {
		if strings.Contains(link.URL, "/working") {
			t.Error("Рабочая ссылка /working попала в битые")
		}
	}
	for _, link := range page.BrokenLinks {
		if strings.Contains(link.URL, "ftp://") {
			t.Error("Ссылка с неподдерживаемой схемой попала в отчет")
		}
	}
}

func TestAnalyze_NoBrokenLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
			<body>
				<a href="/page1">Page 1</a>
				<a href="/page2">Page 2</a>
			</body>
		</html>
		`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	if len(report.Pages) != 1 {
		t.Errorf("Ожидалась 1 страница, получено %d", len(report.Pages))
	}

	if len(report.Pages[0].BrokenLinks) != 0 {
		t.Errorf("Ожидалось 0 битых ссылок, получено %d", len(report.Pages[0].BrokenLinks))
	}
}
func TestAnalyze_SEO_AllElements(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Title</title>
    <meta name="description" content="Test Description">
</head>
<body>
    <h1>Test H1</h1>
    <a href="/page1">Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	if len(report.Pages) == 0 {
		t.Fatal("Отчет не содержит страниц")
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if !seo.HasTitle {
		t.Error("has_title должен быть true")
	}
	if seo.Title != "Test Title" {
		t.Errorf("title ожидался 'Test Title', получен '%s'", seo.Title)
	}

	if !seo.HasDescription {
		t.Error("has_description должен быть true")
	}
	if seo.Description != "Test Description" {
		t.Errorf("description ожидался 'Test Description', получен '%s'", seo.Description)
	}

	if !seo.HasH1 {
		t.Error("has_h1 должен быть true")
	}
	if seo.H1 != "Test H1" {
		t.Errorf("h1 ожидался 'Test H1', получен '%s'", seo.H1)
	}
}

func TestAnalyze_SEO_MissingElements(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
</head>
<body>
    <a href="/page1">Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if seo.HasTitle {
		t.Error("has_title должен быть false")
	}
	if seo.Title != "" {
		t.Errorf("title должен быть пустым, получен '%s'", seo.Title)
	}

	if seo.HasDescription {
		t.Error("has_description должен быть false")
	}
	if seo.Description != "" {
		t.Errorf("description должен быть пустым, получен '%s'", seo.Description)
	}

	if seo.HasH1 {
		t.Error("has_h1 должен быть false")
	}
	if seo.H1 != "" {
		t.Errorf("h1 должен быть пустым, получен '%s'", seo.H1)
	}
}

func TestAnalyze_SEO_EmptyElements(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>   </title>
    <meta name="description" content="   ">
</head>
<body>
    <h1>   </h1>
    <a href="/page1">Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if seo.HasTitle {
		t.Error("has_title должен быть false для пустого title")
	}
	if seo.HasDescription {
		t.Error("has_description должен быть false для пустого description")
	}
	if seo.HasH1 {
		t.Error("has_h1 должен быть false для пустого h1")
	}
}

func TestAnalyze_SEO_HTMLEntities(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test &amp; Title</title>
    <meta name="description" content="Test &amp; Description &copy; 2024">
</head>
<body>
    <h1>Test &amp; H1 &euro;100</h1>
    <a href="/page1">Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	expectedTitle := "Test & Title"
	if seo.Title != expectedTitle {
		t.Errorf("title ожидался '%s', получен '%s'", expectedTitle, seo.Title)
	}

	expectedDesc := "Test & Description © 2024"
	if seo.Description != expectedDesc {
		t.Errorf("description ожидался '%s', получен '%s'", expectedDesc, seo.Description)
	}

	expectedH1 := "Test & H1 €100"
	if seo.H1 != expectedH1 {
		t.Errorf("h1 ожидался '%s', получен '%s'", expectedH1, seo.H1)
	}
}

func TestAnalyze_SEO_MultipleH1(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Title</title>
</head>
<body>
    <h1>First H1</h1>
    <h1>Second H1</h1>
    <a href="/page1">Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(html))
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	seo := report.Pages[0].SEO
	if seo == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if !seo.HasH1 {
		t.Error("has_h1 должен быть true")
	}
	if seo.H1 != "First H1" {
		t.Errorf("h1 должен быть первым заголовком, получен '%s'", seo.H1)
	}
}

func TestAnalyze_SEO_WithBrokenLinks(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Test Title</title>
    <meta name="description" content="Test Description">
</head>
<body>
    <h1>Test H1</h1>
    <a href="/working">Working Link</a>
    <a href="/broken">Broken Link</a>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		case "/working":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		case "/broken":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Not Found"))
		}
	}))
	defer server.Close()

	opts := code.Options{
		URL:        server.URL,
		Depth:      1,
		Timeout:    5 * time.Second,
		IndentJSON: true,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}

	result, err := code.Analyze(context.Background(), opts)
	if err != nil {
		t.Fatalf("Analyze вернул ошибку: %v", err)
	}

	var report code.Report
	err = json.Unmarshal(result, &report)
	if err != nil {
		t.Fatalf("Не удалось распарсить JSON: %v", err)
	}

	page := report.Pages[0]
	
	if page.SEO == nil {
		t.Fatal("SEO данные отсутствуют")
	}

	if !page.SEO.HasTitle {
		t.Error("has_title должен быть true")
	}
	if page.SEO.Title != "Test Title" {
		t.Errorf("title ожидался 'Test Title', получен '%s'", page.SEO.Title)
	}

	if len(page.BrokenLinks) == 0 {
		t.Error("Ожидались битые ссылки")
	}

	foundBroken := false
	for _, link := range page.BrokenLinks {
		if strings.Contains(link.URL, "/broken") {
			foundBroken = true
			if link.StatusCode != 404 {
				t.Errorf("Ожидался статус 404 для /broken, получен %d", link.StatusCode)
			}
			break
		}
	}
	if !foundBroken {
		t.Error("Ссылка /broken не была отмечена как битая")
	}
}