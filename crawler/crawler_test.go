package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeDepthMinusOne(t *testing.T) {
	// Создаем тестовый сервер
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			html := `<!DOCTYPE html>
<html>
   <head>
    <title>Home</title>
    <meta name="description" content="Root page description">
   </head>
   <body>
    <h1>Header</h1>
    <a href="https://example.com/about">About</a>
    <a href="https://example.com/missing">Missing</a>
    <img src="https://example.com/static/logo.png">
    <script src="https://example.com/static/app.js"></script>
    <link rel="stylesheet" href="https://example.com/static/app.css">
   </body>
  </html>`
			w.Write([]byte(html))
		case "/static/logo.png":
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", "16")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("1234567890123456"))
		case "/static/app.js":
			w.Header().Set("Content-Type", "application/javascript")
			w.Header().Set("Content-Length", "32")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("console.log('test'); // 32 bytes total"))
		case "/static/app.css":
			w.Header().Set("Content-Type", "text/css")
			w.Header().Set("Content-Length", "24")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("body { color: red; }"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Создаем второй сервер для внешних ссылок
	exampleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/about":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("About page"))
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer exampleServer.Close()

	// Создаем HTTP клиент с транспортом, который перенаправляет запросы на example.com на наш тестовый сервер
	client := &http.Client{
		Transport: &testTransport{
			exampleServer: exampleServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         server.URL + "/",
		Depth:       1,
		Retries:     1,
		Delay:       0,
		RPS:         0,
		Timeout:     5 * time.Second,
		UserAgent:   "TestBot/1.0",
		Concurrency: 2,
		IndentJSON:  true,
		HTTPClient:  client,
	}

	// Выполняем анализ
	reportData, err := Analyze(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, reportData)

	// Парсим отчет
	var report Report
	err = json.Unmarshal(reportData, &report)
	require.NoError(t, err)

	// Проверяем базовые поля отчета
	assert.Equal(t, server.URL+"/", report.RootURL)
	assert.Equal(t, -1, report.Depth)
	assert.False(t, report.GeneratedAt.IsZero())

	// Проверяем, что есть хотя бы одна страница
	assert.NotEmpty(t, report.Pages)

	// Находим корневую страницу
	var rootPage *Page
	for i, page := range report.Pages {
		if page.URL == server.URL+"/" {
			rootPage = &report.Pages[i]
			break
		}
	}
	require.NotNil(t, rootPage, "Root page not found in report")

	// Проверяем поля корневой страницы
	assert.Equal(t, server.URL+"/", rootPage.URL)
	assert.Equal(t, 0, rootPage.Depth)
	assert.Equal(t, 200, rootPage.HTTPStatus)
	assert.Equal(t, "ok", rootPage.Status)
	assert.Empty(t, rootPage.Error)
	assert.False(t, rootPage.DiscoveredAt.IsZero())

	// Проверяем SEO (обратите внимание, что SEO это указатель)
	require.NotNil(t, rootPage.SEO)
	assert.True(t, rootPage.SEO.HasTitle)
	assert.Equal(t, "Home", rootPage.SEO.Title)
	assert.True(t, rootPage.SEO.HasDescription)
	assert.Equal(t, "Root page description", rootPage.SEO.Description)
	assert.True(t, rootPage.SEO.HasH1)

	// Проверяем битые ссылки
	// Должна быть только missing ссылка (404)
	assert.NotEmpty(t, rootPage.BrokenLinks)

	foundMissing := false
	for _, link := range rootPage.BrokenLinks {
		if link.URL == "https://example.com/missing" {
			foundMissing = true
			assert.Equal(t, 404, link.StatusCode)
			assert.NotEmpty(t, link.Error)
			break
		}
	}
	assert.True(t, foundMissing, "Missing link not found in broken links")

	// Проверяем ассеты
	assert.NotEmpty(t, rootPage.Assets)

	// Ожидаемые ассеты
	expectedAssets := []struct {
		url        string
		assetType  string
		statusCode int
		sizeBytes  int64
	}{
		{"https://example.com/static/logo.png", "image", 200, 16},
		{"https://example.com/static/app.js", "script", 200, 32},
		{"https://example.com/static/app.css", "style", 200, 24},
	}

	for _, expected := range expectedAssets {
		found := false
		for _, asset := range rootPage.Assets {
			if asset.URL == expected.url {
				found = true
				assert.Equal(t, expected.assetType, asset.Type)
				assert.Equal(t, expected.statusCode, asset.StatusCode)
				assert.Equal(t, expected.sizeBytes, asset.SizeBytes)
				break
			}
		}
		assert.True(t, found, "Asset not found: %s", expected.url)
	}
}

// testTransport перенаправляет запросы к example.com на тестовый сервер
type testTransport struct {
	exampleServer *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Перенаправляем запросы к example.com на наш тестовый сервер
	if req.URL.Host == "example.com" {
		// Создаем новый URL с хостом тестового сервера
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.exampleServer.Listener.Addr().String()
		req.URL = &newURL
	}
	return http.DefaultTransport.RoundTrip(req)
}

func TestAnalyzeDepthMinusOneNoChildCrawling(t *testing.T) {
	// Создаем сервер с несколькими страницами
	requestCount := make(map[string]int)
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount[r.URL.Path]++
		
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			html := `<html><body>
				<a href="/page1">Page 1</a>
				<a href="/page2">Page 2</a>
			</body></html>`
			w.Write([]byte(html))
		case "/page1":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("<html><body>Page 1</body></html>"))
		case "/page2":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("<html><body>Page 2</body></html>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         server.URL + "/",
		Depth:       0,
		Retries:     1,
		Timeout:     5 * time.Second,
		UserAgent:   "TestBot/1.0",
		Concurrency: 2,
		HTTPClient:  client,
		IndentJSON:  true,
	}

	reportData, err := Analyze(context.Background(), opts)
	require.NoError(t, err)

	var report Report
	err = json.Unmarshal(reportData, &report)
	require.NoError(t, err)

	// Должна быть только одна страница - корневая
	assert.Equal(t, 1, len(report.Pages))
	assert.Equal(t, server.URL+"/", report.Pages[0].URL)

	// Проверяем, что запросы к дочерним страницам не выполнялись
	assert.Equal(t, 1, requestCount["/"]) // Корневая страница запрошена 1 раз
	assert.Equal(t, 0, requestCount["/page1"])
	assert.Equal(t, 0, requestCount["/page2"])
}
