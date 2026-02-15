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