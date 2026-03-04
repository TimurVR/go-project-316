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

func TestAnalyzeDepthOne(t *testing.T) {
	mainServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
    <a href="/about">About</a>
    <a href="/missing">Missing</a>
    <a href="https://external.com">External</a>
    <img src="/static/logo.png">
    <script src="/static/app.js"></script>
    <link rel="stylesheet" href="/static/app.css">
   </body>
  </html>`
			w.Write([]byte(html))
		case "/about":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			html := `<!DOCTYPE html>
<html>
   <head>
    <title>About</title>
    <meta name="description" content="About page description">
   </head>
   <body>
    <h1>About Us</h1>
    <a href="/">Back to Home</a>
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
	defer mainServer.Close()

	missingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missingServer.Close()

	externalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body>External site</body></html>"))
	}))
	defer externalServer.Close()

	client := &http.Client{
		Transport: &testTransport{
			mainServer:     mainServer,
			missingServer:  missingServer,
			externalServer: externalServer,
		},
		Timeout: 5 * time.Second,
	}

	opts := Options{
		URL:         mainServer.URL + "/",
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

	reportData, err := Analyze(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, reportData)

	var report Report
	err = json.Unmarshal(reportData, &report)
	require.NoError(t, err)

	assert.Equal(t, mainServer.URL+"/", report.RootURL)
	assert.Equal(t, 1, report.Depth)
	assert.False(t, report.GeneratedAt.IsZero())

	assert.Equal(t, 1, len(report.Pages), "Expected 1 page (root only)")

	rootPage := report.Pages[0]
	assert.Equal(t, mainServer.URL+"/", rootPage.URL)
	assert.Equal(t, 0, rootPage.Depth)
	assert.Equal(t, 200, rootPage.HTTPStatus)
	assert.Equal(t, "ok", rootPage.Status)
	assert.Empty(t, rootPage.Error)
	assert.False(t, rootPage.DiscoveredAt.IsZero())

	require.NotNil(t, rootPage.SEO)
	assert.True(t, rootPage.SEO.HasTitle)
	assert.Equal(t, "Home", rootPage.SEO.Title)
	assert.True(t, rootPage.SEO.HasDescription)
	assert.Equal(t, "Root page description", rootPage.SEO.Description)
	assert.True(t, rootPage.SEO.HasH1)

	assert.NotEmpty(t, rootPage.BrokenLinks)
	foundMissing := false
	for _, link := range rootPage.BrokenLinks {
		if link.URL == mainServer.URL+"/missing" {
			foundMissing = true
			assert.Equal(t, 404, link.StatusCode)
			assert.NotEmpty(t, link.Error)
			break
		}
	}
	assert.True(t, foundMissing, "Missing link not found in broken links")

	assert.NotEmpty(t, rootPage.Assets)
	expectedAssets := []struct {
		url        string
		assetType  string
		statusCode int
		sizeBytes  int64
	}{
		{mainServer.URL + "/static/logo.png", "image", 200, 16},
		{mainServer.URL + "/static/app.js", "script", 200, 32},
		{mainServer.URL + "/static/app.css", "style", 200, 24},
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

	for _, page := range report.Pages {
		assert.NotEqual(t, mainServer.URL+"/about", page.URL, "Page /about should not be in report at depth 1")
	}
}

type testTransport struct {
	mainServer     *httptest.Server
	missingServer  *httptest.Server
	externalServer *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == t.mainServer.Listener.Addr().String() {
		return http.DefaultTransport.RoundTrip(req)
	}
	
	if req.URL.Path == "/missing" {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.missingServer.Listener.Addr().String()
		req.URL = &newURL
		return http.DefaultTransport.RoundTrip(req)
	}
	
	if req.URL.Host == "external.com" {
		newURL := *req.URL
		newURL.Scheme = "http"
		newURL.Host = t.externalServer.Listener.Addr().String()
		req.URL = &newURL
		return http.DefaultTransport.RoundTrip(req)
	}
	
	return http.DefaultTransport.RoundTrip(req)
}