package code

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Options struct {
	URL         string
	Depth       int
	Retries     int
	Delay       time.Duration
	Timeout     time.Duration
	UserAgent   string
	Concurrency int
	IndentJSON  bool
	HTTPClient  *http.Client
}
type BrokenLink struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code,omitempty"`
	Error      string `json:"error,omitempty"`
}
type Page struct {
	URL          string       `json:"url"`
	Depth        int          `json:"depth"`
	HTTPStatus   int          `json:"http_status"`
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	BrokenLinks  []BrokenLink `json:"broken_links,omitempty"`
	DiscoveredAt time.Time    `json:"discovered_at"`
}

type Report struct {
	RootURL     string    `json:"root_url"`
	Depth       int       `json:"depth"`
	GeneratedAt time.Time `json:"generated_at"`
	Pages       []Page    `json:"pages"`
}

func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL обязателен")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: opts.Timeout,
		}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	html, err := GetHTMLWithContext(ctx, opts.URL)
	if err != nil {
		return nil, fmt.Errorf("Проблема в получении HTML: %w", err)
	}
	rawLinks := extractLinks(html)
	baseURL, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("Ошибка парсинга базового URL: %w", err)
	}
	absoluteLinks := make([]string, 0)
	for _, link := range rawLinks {
		absLink, err := normalizeURL(link, baseURL)
		if err != nil {
			continue
		}
		if absLink != "" && shouldCheckLink(absLink) {
			absoluteLinks = append(absoluteLinks, absLink)
		}
	}
	brokenLinks := make([]BrokenLink, 0)
	for _, link := range absoluteLinks {
		statusCode, err := checkLink(ctx, opts.HTTPClient, link, opts.UserAgent)
		if err != nil || statusCode >= 400 {
			brokenLink := BrokenLink{
				URL: link,
			}
			if err != nil {
				brokenLink.Error = err.Error()
			} else {
				brokenLink.StatusCode = statusCode
			}
			brokenLinks = append(brokenLinks, brokenLink)
		}
	}
	mainPageStatus, mainPageErr := checkLink(ctx, opts.HTTPClient, opts.URL, opts.UserAgent)
	
	report := Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages: []Page{
			{
				URL:          opts.URL,
				Depth:        0,
				DiscoveredAt: time.Now().UTC(),
				BrokenLinks:  brokenLinks,
			},
		},
	}
	if mainPageErr != nil {
		report.Pages[0].Status = "error"
		report.Pages[0].Error = mainPageErr.Error()
		report.Pages[0].HTTPStatus = 0
	} else {
		report.Pages[0].Status = "ok"
		report.Pages[0].HTTPStatus = mainPageStatus
	}
	var jsonData []byte
	if opts.IndentJSON {
		jsonData, err = json.MarshalIndent(report, "", "  ")
	} else {
		jsonData, err = json.Marshal(report)
	}
	
	if err != nil {
		return nil, fmt.Errorf("Ошибка JSON: %w", err)
	}
	
	return jsonData, nil
}

func extractLinks(html string) []string {
	links := make([]string, 0)
	lines := strings.Split(html, "\n")
	
	for _, line := range lines {
		if strings.Contains(line, "href=") {
			parts := strings.Split(line, "href=\"")
			if len(parts) > 1 {
				href := strings.Split(parts[1], "\"")[0]
				if href != "" {
					links = append(links, href)
				}
			}
		}
	}
	
	return links
}

func normalizeURL(rawURL string, base *url.URL) (string, error) {
	if rawURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("неподдерживаемая схема: %s", parsed.Scheme)
		}
		return parsed.String(), nil
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return "", fmt.Errorf("неподдерживаемая схема: %s", resolved.Scheme)
	}
	
	return resolved.String(), nil
}

func shouldCheckLink(rawURL string) bool {
	if rawURL == "" || strings.HasPrefix(rawURL, "#") {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	
	return true
}

func checkLink(ctx context.Context, client *http.Client, linkURL, userAgent string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", linkURL, nil)
	if err != nil {
		return 0, fmt.Errorf("создание запроса: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", "HexletGoCrawler/1.0")
	}
	resp, err := client.Do(req)
	if err != nil {
		return checkLinkWithGET(ctx, client, linkURL, userAgent)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil
	}
	
	return resp.StatusCode, nil
}

func checkLinkWithGET(ctx context.Context, client *http.Client, linkURL, userAgent string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", linkURL, nil)
	if err != nil {
		return 0, fmt.Errorf("создание GET запроса: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", "HexletGoCrawler/1.0")
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.CopyN(io.Discard, resp.Body, 1)
	return resp.StatusCode, nil
}

func GetHTMLWithContext(ctx context.Context, url string) (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("Создание запроса: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Выполнение запроса: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Статус код: %d %s", resp.StatusCode, resp.Status)
	}
	contentType := resp.Header.Get("Content-Type")
	if !isHTMLContent(contentType) {
		return "", fmt.Errorf("Не HTML контент: %s", contentType)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Чтение тела: %w", err)
	}
	return string(body), nil
}

func isHTMLContent(contentType string) bool {
	return len(contentType) >= 9 && contentType[:9] == "text/html"
}