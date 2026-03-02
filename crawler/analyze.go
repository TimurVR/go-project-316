package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type SEO struct {
	HasTitle       bool   `json:"has_title"`
	Title          string `json:"title"`
	HasDescription bool   `json:"has_description"`
	Description    string `json:"description"`
	HasH1          bool   `json:"has_h1"`
}

type Asset struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	StatusCode int    `json:"status_code"`
	SizeBytes  int64  `json:"size_bytes"`
}

type Options struct {
	URL         string
	Depth       int
	Retries     int
	Delay       time.Duration
	RPS         float64
	Timeout     time.Duration
	UserAgent   string
	Concurrency int
	IndentJSON  bool
	HTTPClient  *http.Client
}

type BrokenLink struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Error      string `json:"error"`
}

type Page struct {
	URL          string       `json:"url"`
	Depth        int          `json:"depth"`
	HTTPStatus   int          `json:"http_status"`
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	BrokenLinks  []BrokenLink `json:"broken_links"`
	SEO          *SEO         `json:"seo"`
	Assets       []Asset      `json:"assets"`
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

type RateLimiter struct {
	lastRequest time.Time
	minInterval time.Duration
	mu          sync.Mutex
}

type assetCacheItem struct {
	asset Asset
}

var (
	assetCache = make(map[string]assetCacheItem)
	assetMu    sync.RWMutex
)

func NewRateLimiter(delay time.Duration, rps float64) *RateLimiter {
	var minInterval time.Duration
	if rps > 0 {
		minInterval = time.Duration(float64(time.Second) / rps)
	} else if delay > 0 {
		minInterval = delay
	} else {
		return nil
	}
	return &RateLimiter{
		minInterval: minInterval,
		lastRequest: time.Now().Add(-minInterval),
	}
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(rl.lastRequest)
	if elapsed < rl.minInterval {
		waitTime := rl.minInterval - elapsed
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
	rl.lastRequest = time.Now()
	return nil
}

func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("URL обязателен")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 1
	}

	rawRoot, _ := NormalizeURL(opts.URL, nil)
	rootURL, _ := url.Parse(rawRoot)

	rateLimiter := NewRateLimiter(opts.Delay, opts.RPS)
	report := Report{
		RootURL:     rawRoot,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       make([]Page, 0),
	}

	visited := make(map[string]bool)
	visitedMu := sync.Mutex{}
	pagesMu := sync.Mutex{}
	pagesMap := make(map[string]Page)

	taskChan := make(chan CrawlTask, 10000)
	var activeTasks sync.WaitGroup
	var workersWg sync.WaitGroup

	assetMu.Lock()
	assetCache = make(map[string]assetCacheItem)
	assetMu.Unlock()

	for i := 0; i < opts.Concurrency; i++ {
		workersWg.Add(1)
		go func() {
			defer workersWg.Done()
			for task := range taskChan {
				if task.Depth > opts.Depth {
					activeTasks.Done()
					continue
				}

				if err := rateLimiter.Wait(ctx); err != nil {
					activeTasks.Done()
					continue
				}
				page, _ := crawlPage(ctx, opts, task.URL, task.Depth)

				pagesMu.Lock()
				pagesMap[page.URL] = page
				pagesMu.Unlock()
				if task.Depth < opts.Depth && page.Status == "ok" {
					html, err := GetHTMLWithContext(ctx, page.URL, opts.HTTPClient, opts.UserAgent)
					if err == nil {
						baseURL, _ := url.Parse(page.URL)
						for _, link := range extractLinks(html) {
							absLink, _ := NormalizeURL(link, baseURL)
							if absLink == "" {
								continue
							}

							linkURL, _ := url.Parse(absLink)
							if IsSameDomain(linkURL, rootURL) {
								visitedMu.Lock()
								if !visited[absLink] {
									visited[absLink] = true
									activeTasks.Add(1)
									taskChan <- CrawlTask{URL: absLink, Depth: task.Depth + 1}
								}
								visitedMu.Unlock()
							}
						}
					}
				}
				activeTasks.Done()
			}
		}()
	}

	visited[rawRoot] = true
	activeTasks.Add(1)
	taskChan <- CrawlTask{URL: rawRoot, Depth: 0}

	go func() {
		activeTasks.Wait()
		close(taskChan)
	}()

	workersWg.Wait()

	for _, page := range pagesMap {
		report.Pages = append(report.Pages, page)
	}

	sort.Slice(report.Pages, func(i, j int) bool {
		return report.Pages[i].URL < report.Pages[j].URL
	})

	if opts.IndentJSON {
		return json.MarshalIndent(report, "", "  ")
	}
	return json.Marshal(report)
}

func GetHTMLWithContext(ctx context.Context, urlStr string, client *http.Client, ua string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body), nil
}

func extractSEO(html string) *SEO {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return &SEO{}
	}
	seo := &SEO{}
	titleTag := doc.Find("title").First()
	if titleTag.Length() > 0 {
		seo.Title = strings.TrimSpace(titleTag.Text())
		seo.HasTitle = seo.Title != ""
	}
	desc, exists := doc.Find("meta[name='description']").Attr("content")
	if exists {
		seo.Description = strings.TrimSpace(desc)
		seo.HasDescription = seo.Description != ""
	}
	seo.HasH1 = doc.Find("h1").Length() > 0
	return seo
}

func extractLinks(html string) []string {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	var links []string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			links = append(links, href)
		}
	})
	return links
}

func ExtractAssetURLs(html string) []string {
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(html))
	var assets []string
	doc.Find("img, script, link[rel='stylesheet']").Each(func(i int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok {
			assets = append(assets, src)
		} else if href, ok := s.Attr("href"); ok {
			assets = append(assets, href)
		}
	})
	return assets
}

func FetchAsset(ctx context.Context, client *http.Client, urlStr string, ua string) (Asset, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return Asset{}, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Asset{}, err
	}
	defer resp.Body.Close()

	assetType := "unknown"
	if strings.Contains(urlStr, ".js") {
		assetType = "script"
	} else if strings.Contains(urlStr, ".css") {
		assetType = "style"
	} else if strings.Contains(urlStr, ".png") || strings.Contains(urlStr, ".jpg") || strings.Contains(urlStr, ".svg") {
		assetType = "image"
	}

	return Asset{
		URL:        urlStr,
		Type:       assetType,
		StatusCode: resp.StatusCode,
		SizeBytes:  resp.ContentLength,
	}, nil
}

func NormalizeURL(href string, base *url.URL) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	var resolved *url.URL
	if base != nil {
		resolved = base.ResolveReference(u)
	} else {
		resolved = u
	}
	resolved.Fragment = ""
	resStr := resolved.String()
	if strings.HasSuffix(resStr, "/") && len(resStr) > (len(resolved.Scheme)+3+len(resolved.Host)) {
		resStr = strings.TrimSuffix(resStr, "/")
	}
	return resStr, nil
}

func IsSameDomain(u1, u2 *url.URL) bool {
	return u1.Host == u2.Host
}

func ShouldCheckAsset(urlStr string) bool {
	return urlStr != "" && (strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://"))
}

func crawlPage(ctx context.Context, opts Options, pageURL string, depth int) (Page, error) {
	page := Page{
		URL:          pageURL,
		Depth:        depth,
		DiscoveredAt: time.Now().UTC(),
		SEO:          &SEO{},
	}

	html, err := GetHTMLWithContext(ctx, pageURL, opts.HTTPClient, opts.UserAgent)
	if err != nil {
		page.Status = "error"
		page.Error = err.Error()
		return page, nil
	}

	page.SEO = extractSEO(html)
	page.Status = "ok"
	page.HTTPStatus = 200
	page.BrokenLinks = make([]BrokenLink, 0)
	page.Assets = make([]Asset, 0)

	baseURL, _ := url.Parse(pageURL)
	rawAssets := ExtractAssetURLs(html)

	var assets []Asset
	for _, assetURL := range rawAssets {
		abs, _ := NormalizeURL(assetURL, baseURL)
		if !ShouldCheckAsset(abs) {
			continue
		}

		assetMu.RLock()
		cached, exists := assetCache[abs]
		assetMu.RUnlock()

		if exists {
			assets = append(assets, cached.asset)
			continue
		}

		asset, err := FetchAsset(ctx, opts.HTTPClient, abs, opts.UserAgent)
		if err == nil {
			assetMu.Lock()
			assetCache[abs] = assetCacheItem{asset: asset}
			assetMu.Unlock()
			assets = append(assets, asset)
		}
	}

	sort.Slice(assets, func(i, j int) bool {
		if assets[i].Type != assets[j].Type {
			return assets[i].Type < assets[j].Type
		}
		return assets[i].URL < assets[j].URL
	})

	if len(assets) > 0 {
		page.Assets = assets
	}

	return page, nil
}
