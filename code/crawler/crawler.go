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

	"golang.org/x/net/html"
)

type SEO struct {
	HasTitle       bool   `json:"has_title"`
	Title          string `json:"title"`
	HasDescription bool   `json:"has_description"`
	Description    string `json:"description"`
	HasH1          bool   `json:"has_h1"`
	H1             string `json:"h1,omitempty"`
}

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
	SEO          *SEO         `json:"seo,omitempty"`
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

	seoData := extractSEO(html)

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
				SEO:          seoData,
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

func extractSEO(htmlContent string) *SEO {
	seo := &SEO{
		HasTitle:       false,
		Title:          "",
		HasDescription: false,
		Description:    "",
		HasH1:          false,
		H1:             "",
	}

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return seo
	}

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if n.FirstChild != nil {
					title := strings.TrimSpace(n.FirstChild.Data)
					title = decodeHTMLEntities(title)
					if title != "" {
						seo.HasTitle = true
						seo.Title = title
					}
				}
			case "h1":
				if !seo.HasH1 && n.FirstChild != nil {
					h1 := extractText(n)
					h1 = strings.TrimSpace(h1)
					h1 = decodeHTMLEntities(h1)
					if h1 != "" {
						seo.HasH1 = true
						seo.H1 = h1
					}
				}
			case "meta":
				var name, content string
				for _, attr := range n.Attr {
					if attr.Key == "name" && strings.ToLower(attr.Val) == "description" {
						name = attr.Val
					}
					if attr.Key == "content" {
						content = attr.Val
					}
				}
				if name == "description" && content != "" {
					content = strings.TrimSpace(content)
					content = decodeHTMLEntities(content)
					if content != "" {
						seo.HasDescription = true
						seo.Description = content
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)
	return seo
}

func extractText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += extractText(c)
	}
	return text
}

func decodeHTMLEntities(s string) string {
	replacements := map[string]string{
		"&amp;":   "&",
		"&lt;":    "<",
		"&gt;":    ">",
		"&quot;":  "\"",
		"&#39;":   "'",
		"&nbsp;":  " ",
		"&copy;":  "©",
		"&reg;":   "®",
		"&trade;": "™",
		"&euro;":  "€",
		"&pound;": "£",
		"&yen;":   "¥",
		"&cent;":  "¢",
		"&sect;":  "§",
		"&deg;":   "°",
	}

	for entity, replacement := range replacements {
		s = strings.ReplaceAll(s, entity, replacement)
	}

	return s
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
