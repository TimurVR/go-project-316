package code

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
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

type CrawlTask struct {
	URL   string
	Depth int
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
	if opts.Concurrency == 0 {
		opts.Concurrency = 1
	}
	
	rootURL, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("Ошибка парсинга URL: %w", err)
	}
	
	report := Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       make([]Page, 0),
	}
	
	visited := make(map[string]bool)
	visitedMu := sync.Mutex{}
	
	taskChan := make(chan CrawlTask, 100)
	resultChan := make(chan Page, 100)
	errorChan := make(chan error, 100)
	var wg sync.WaitGroup
	
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					page, err := crawlPage(ctx, opts, task.URL, task.Depth, rootURL)
					if err != nil {
						errorChan <- err
						continue
					}
					resultChan <- page
				}
			}
		}()
	}
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		taskChan <- CrawlTask{URL: opts.URL, Depth: 0}
		visited[opts.URL] = true
	}()
	
	go func() {
		wg.Wait()
		close(taskChan)
		close(resultChan)
		close(errorChan)
	}()
	
	pagesMap := make(map[string]Page)
	
	for {
		select {
		case <-ctx.Done():
			goto FINISH
		case page, ok := <-resultChan:
			if !ok {
				goto FINISH
			}
			pagesMap[page.URL] = page
			
			if page.Depth < opts.Depth {
				for _, link := range extractLinksFromPage(page.URL, page.SEO) {
					absLink, err := normalizeURL(link, rootURL)
					if err != nil {
						continue
					}
					
					linkURL, err := url.Parse(absLink)
					if err != nil {
						continue
					}
					
					if isSameDomain(linkURL, rootURL) {
						visitedMu.Lock()
						if !visited[absLink] {
							visited[absLink] = true
							taskChan <- CrawlTask{URL: absLink, Depth: page.Depth + 1}
						}
						visitedMu.Unlock()
					}
				}
			}
		case err := <-errorChan:
			fmt.Printf("Ошибка при обходе: %v\n", err)
		}
	}
	
FINISH:
	for _, page := range pagesMap {
		report.Pages = append(report.Pages, page)
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

func crawlPage(ctx context.Context, opts Options, pageURL string, depth int, rootURL *url.URL) (Page, error) {
	page := Page{
		URL:          pageURL,
		Depth:        depth,
		DiscoveredAt: time.Now().UTC(),
		BrokenLinks:  make([]BrokenLink, 0),
	}
	
	html, err := GetHTMLWithContext(ctx, pageURL)
	if err != nil {
		page.Status = "error"
		page.Error = err.Error()
		page.HTTPStatus = 0
		return page, nil
	}
	
	seoData := extractSEO(html)
	page.SEO = seoData
	page.Status = "ok"
	page.HTTPStatus = 200
	
	rawLinks := extractLinks(html)
	baseURL, err := url.Parse(pageURL)
	if err != nil {
		return page, nil
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
	
	for _, link := range absoluteLinks {
		linkURL, err := url.Parse(link)
		if err != nil {
			continue
		}
		
		if !isSameDomain(linkURL, rootURL) {
			continue
		}
		
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
			page.BrokenLinks = append(page.BrokenLinks, brokenLink)
		}
	}
	
	return page, nil
}

func extractLinksFromPage(pageURL string, seo *SEO) []string {
	links := make([]string, 0)
	return links
}

func isSameDomain(linkURL, rootURL *url.URL) bool {
	return linkURL.Host == rootURL.Host
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
	
	titleStart := strings.Index(htmlContent, "<title>")
	titleEnd := strings.Index(htmlContent, "</title>")
	if titleStart != -1 && titleEnd != -1 && titleEnd > titleStart {
		title := htmlContent[titleStart+7 : titleEnd]
		title = strings.TrimSpace(title)
		title = decodeHTMLEntities(title)
		if title != "" {
			seo.HasTitle = true
			seo.Title = title
		}
	}
	
	descPattern := "<meta name=\"description\" content=\""
	descStart := strings.Index(htmlContent, descPattern)
	if descStart != -1 {
		contentStart := descStart + len(descPattern)
		contentEnd := strings.Index(htmlContent[contentStart:], "\"")
		if contentEnd != -1 {
			description := htmlContent[contentStart : contentStart+contentEnd]
			description = strings.TrimSpace(description)
			description = decodeHTMLEntities(description)
			if description != "" {
				seo.HasDescription = true
				seo.Description = description
			}
		}
	}
	
	h1Start := strings.Index(htmlContent, "<h1>")
	if h1Start == -1 {
		h1Start = strings.Index(htmlContent, "<h1 ")
	}
	if h1Start != -1 {
		h1End := strings.Index(htmlContent[h1Start:], "</h1>")
		if h1End != -1 {
			h1Content := htmlContent[h1Start:]
			gtPos := strings.Index(h1Content, ">")
			if gtPos != -1 {
				h1Content = h1Content[gtPos+1 : h1End]
				h1Content = strings.TrimSpace(h1Content)
				h1Content = decodeHTMLEntities(h1Content)
				if h1Content != "" {
					seo.HasH1 = true
					seo.H1 = h1Content
				}
			}
		}
	}
	
	return seo
}

func decodeHTMLEntities(s string) string {
	replacements := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": "\"",
		"&#39;":  "'",
		"&nbsp;": " ",
		"&copy;": "©",
		"&reg;":  "®",
		"&trade;": "™",
		"&euro;": "€",
		"&pound;": "£",
		"&yen;":  "¥",
		"&cent;": "¢",
		"&sect;": "§",
		"&deg;":  "°",
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