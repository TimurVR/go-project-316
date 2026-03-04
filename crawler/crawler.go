package crawler

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

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"golang.org/x/time/rate"
)

func newAssetCache() *assetCache {
	return &assetCache{
		assets: make(map[string]*Asset),
	}
}

func (ac *assetCache) get(url string) (*Asset, bool) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	asset, ok := ac.assets[url]
	return asset, ok
}

func (ac *assetCache) set(url string, asset *Asset) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.assets[url] = asset
}

type task struct {
	url   string
	depth int
}

type rateLimiter struct {
	limiter  *rate.Limiter
	delay    time.Duration
	useDelay bool
	mu       sync.Mutex
	lastTime time.Time
}

func newRateLimiter(opts Options) *rateLimiter {
	rl := &rateLimiter{
		useDelay: opts.RPS == 0 && opts.Delay > 0,
		lastTime: time.Now(),
	}

	if opts.RPS > 0 {
		rl.limiter = rate.NewLimiter(rate.Limit(opts.RPS), 1)
	} else if opts.Delay > 0 {
		rl.delay = opts.Delay
	}

	return rl
}

func (rl *rateLimiter) Wait(ctx context.Context) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.limiter != nil {
		return rl.limiter.Wait(ctx)
	}

	if rl.useDelay {
		now := time.Now()
		elapsed := now.Sub(rl.lastTime)
		if elapsed < rl.delay {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(rl.delay - elapsed):
			}
		}
		rl.lastTime = time.Now()
	}

	return nil
}

func shouldRetry(err error, statusCode int) bool {
	if err != nil {
		return true
	}

	switch statusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func doRequestWithRetry(ctx context.Context, opts Options, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= opts.Retries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err = opts.HTTPClient.Do(req)

		if err == nil {
			if !shouldRetry(err, resp.StatusCode) {
				return resp, nil
			}
			resp.Body.Close()
		}

		if attempt == opts.Retries {
			if err != nil {
				return nil, fmt.Errorf("request failed after %d retries: %w", opts.Retries, err)
			}
			return nil, fmt.Errorf("request failed after %d retries with status %d", opts.Retries, resp.StatusCode)
		}

		waitDuration := opts.Delay
		if waitDuration == 0 {
			waitDuration = time.Duration(100*(1<<attempt)) * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(waitDuration):
		}
	}

	return nil, fmt.Errorf("unexpected error in retry logic")
}

func getAssetType(url string, element string, attrs map[string]string) string {
	lowerURL := strings.ToLower(url)

	if strings.Contains(lowerURL, ".jpg") || strings.Contains(lowerURL, ".jpeg") ||
		strings.Contains(lowerURL, ".png") || strings.Contains(lowerURL, ".gif") ||
		strings.Contains(lowerURL, ".svg") || strings.Contains(lowerURL, ".webp") ||
		strings.Contains(lowerURL, ".ico") {
		return "image"
	}

	if strings.Contains(lowerURL, ".js") {
		return "script"
	}

	if strings.Contains(lowerURL, ".css") {
		return "style"
	}

	switch element {
	case "img":
		return "image"
	case "script":
		return "script"
	case "link":
		if rel, ok := attrs["rel"]; ok && rel == "stylesheet" {
			return "style"
		}
	}

	return "other"
}

func fetchAsset(ctx context.Context, opts Options, assetURL string, assetType string, cache *assetCache) *Asset {
	if cached, ok := cache.get(assetURL); ok {
		return cached
	}

	asset := &Asset{
		URL:        assetURL,
		Type:       assetType,
		StatusCode: 0,
		SizeBytes:  0,
		Error:      "",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		asset.Error = fmt.Sprintf("failed to create request: %v", err)
		cache.set(assetURL, asset)
		return asset
	}

	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}

	resp, err := doRequestWithRetry(ctx, opts, req)
	if err != nil {
		asset.Error = err.Error()
		cache.set(assetURL, asset)
		return asset
	}
	defer resp.Body.Close()

	asset.StatusCode = resp.StatusCode

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		if resp.ContentLength > 0 {
			asset.SizeBytes = resp.ContentLength
		} else {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				asset.Error = fmt.Sprintf("failed to read body: %v", err)
			} else {
				asset.SizeBytes = int64(len(body))
			}
		}
	} else {
		asset.Error = fmt.Sprintf("HTTP status: %d", resp.StatusCode)
	}

	cache.set(assetURL, asset)
	return asset
}

func extractAssets(ctx context.Context, opts Options, doc *goquery.Document, baseURL string, cache *assetCache) []Asset {
	assets := make([]Asset, 0)
	seen := make(map[string]bool)

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists || src == "" {
			return
		}

		absURL := resolveURL(src, baseURL)
		if absURL == "" || seen[absURL] {
			return
		}
		seen[absURL] = true

		asset := fetchAsset(ctx, opts, absURL, "image", cache)
		assets = append(assets, *asset)
	})

	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		src, exists := s.Attr("src")
		if !exists || src == "" {
			return
		}

		absURL := resolveURL(src, baseURL)
		if absURL == "" || seen[absURL] {
			return
		}
		seen[absURL] = true

		asset := fetchAsset(ctx, opts, absURL, "script", cache)
		assets = append(assets, *asset)
	})

	doc.Find("link[rel='stylesheet']").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists || href == "" {
			return
		}

		absURL := resolveURL(href, baseURL)
		if absURL == "" || seen[absURL] {
			return
		}
		seen[absURL] = true

		asset := fetchAsset(ctx, opts, absURL, "style", cache)
		assets = append(assets, *asset)
	})

	return assets
}

func resolveURL(ref, base string) string {
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}

	refURL, err := url.Parse(ref)
	if err != nil {
		return ""
	}

	absURL := baseURL.ResolveReference(refURL)

	if absURL.Scheme != "http" && absURL.Scheme != "https" {
		return ""
	}

	absURL.Fragment = ""
	return absURL.String()
}

func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Timeout: opts.Timeout,
		}
	}

	rootURL, err := url.Parse(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid root URL: %w", err)
	}

	limiter := newRateLimiter(opts)
	assetCache := newAssetCache()

	report := Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       make([]Page, 0),
	}

	visited := make(map[string]bool)
	visitedMutex := &sync.RWMutex{}

	tasks := make(chan crawlTask, 100)
	results := make(chan Page, 100)

	var wg sync.WaitGroup
	var resultsWg sync.WaitGroup

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go worker(workerCtx, opts, rootURL, limiter, assetCache, tasks, results, &wg)
	}
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for page := range results {
			visitedMutex.Lock()
			if !visited[page.URL] {
				visited[page.URL] = true
				report.Pages = append(report.Pages, page)
			}
			visitedMutex.Unlock()
		}
	}()
	select {
	case tasks <- crawlTask{url: opts.URL, depth: 0}:
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	go func() {
		wg.Wait()
		close(tasks)
	}()
	go func() {
		wg.Wait()
		close(results)
	}()
	done := make(chan struct{})
	go func() {
		resultsWg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	case <-done:
	}
	var jsonData []byte
	if opts.IndentJSON {
		jsonData, err = json.MarshalIndent(report, "", "  ")
	} else {
		jsonData, err = json.Marshal(report)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	return jsonData, nil
}

func worker(ctx context.Context, opts Options, rootURL *url.URL, limiter *rateLimiter,
	assetCache *assetCache, tasks chan crawlTask, results chan<- Page, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-tasks:
			if !ok {
				return
			}

			if t.depth > opts.Depth {
				continue
			}

			pageURL, err := url.Parse(t.url)
			if err != nil {
				continue
			}

			if pageURL.Host != rootURL.Host {
				continue
			}

			if err := limiter.Wait(ctx); err != nil {
				if err == context.Canceled {
					return
				}
				continue
			}

			page, err := fetchPage(ctx, opts, t.url, t.depth, assetCache)
			if err != nil {
				page = Page{
					URL:          t.url,
					Depth:        t.depth,
					HTTPStatus:   0,
					Status:       "error",
					Error:        err.Error(),
					BrokenLinks:  []BrokenLink{},
					Assets:       []Asset{},
					SEO:          SEO{HasTitle: false, Title: "", HasDescription: false, Description: "", HasH1: false, H1: ""},
					DiscoveredAt: time.Now().UTC(),
				}
			}

			select {
			case results <- page:
			case <-ctx.Done():
				return
			}
			if t.depth < opts.Depth && page.Status == "ok" {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
				if err != nil {
					continue
				}

				if opts.UserAgent != "" {
					req.Header.Set("User-Agent", opts.UserAgent)
				}

				resp, err := doRequestWithRetry(ctx, opts, req)
				if err != nil {
					continue
				}

				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					continue
				}
				links, err := extractLinksFromHTML(body, t.url)
				if err != nil {
					continue
				}
				for _, link := range links {
					linkURL, err := url.Parse(link)
					if err != nil {
						continue
					}

					if linkURL.Host == rootURL.Host {
						linkURL.Fragment = ""
						normalizedLink := linkURL.String()

						if t.depth+1 <= opts.Depth {
							select {
							case tasks <- crawlTask{url: normalizedLink, depth: t.depth + 1}:
							case <-ctx.Done():
								return
							}
						}
					}
				}
			}
		}
	}
}

func extractLinksFromPage(ctx context.Context, opts Options, rootURL *url.URL,
	pageURL string, limiter *rateLimiter, assetCache *assetCache) ([]string, error) {
	if err := limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}

	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}

	resp, err := doRequestWithRetry(ctx, opts, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	links, err := extractLinksFromHTML(body, pageURL)
	if err != nil {
		return nil, err
	}

	internalLinks := make([]string, 0)
	for _, link := range links {
		linkURL, err := url.Parse(link)
		if err != nil {
			continue
		}

		if linkURL.Host == rootURL.Host {
			linkURL.Fragment = ""
			internalLinks = append(internalLinks, linkURL.String())
		}
	}

	return internalLinks, nil
}

func fetchPage(ctx context.Context, opts Options, pageURL string, depth int,
	assetCache *assetCache) (Page, error) {
	page := Page{
		URL:          pageURL,
		Depth:        depth,
		HTTPStatus:   0,
		Status:       "",
		Error:        "",
		BrokenLinks:  []BrokenLink{},
		Assets:       []Asset{},
		SEO:          SEO{HasTitle: false, Title: "", HasDescription: false, Description: "", HasH1: false, H1: ""},
		DiscoveredAt: time.Now().UTC(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		page.Status = "error"
		page.Error = fmt.Sprintf("failed to create request: %v", err)
		return page, nil
	}

	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}

	resp, err := doRequestWithRetry(ctx, opts, req)
	if err != nil {
		page.Status = "error"
		page.Error = fmt.Sprintf("request failed: %v", err)
		return page, nil
	}
	defer resp.Body.Close()

	page.HTTPStatus = resp.StatusCode

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		page.Status = "ok"

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			page.Error = fmt.Sprintf("failed to read response body: %v", err)
			return page, nil
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			page.Error = fmt.Sprintf("failed to parse HTML: %v", err)
			return page, nil
		}

		page.SEO = extractSEOFromDoc(doc)
		page.Assets = extractAssets(ctx, opts, doc, pageURL, assetCache)

		allLinks, err := extractLinksFromHTML(body, pageURL)
		if err != nil {
			page.Error = fmt.Sprintf("failed to parse links: %v", err)
			return page, nil
		}

		for _, link := range allLinks {
			brokenLink := checkLinkWithRetry(ctx, opts, link)
			if brokenLink != nil {
				page.BrokenLinks = append(page.BrokenLinks, *brokenLink)
			}
		}
	} else {
		page.Status = "error"
		page.Error = fmt.Sprintf("HTTP status: %d", resp.StatusCode)
	}

	return page, nil
}

func checkLinkWithRetry(ctx context.Context, opts Options, linkURL string) *BrokenLink {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, linkURL, nil)
	if err != nil {
		return &BrokenLink{
			URL:        linkURL,
			StatusCode: 0,
			Error:      fmt.Sprintf("failed to create request: %v", err),
		}
	}

	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	}

	resp, err := doRequestWithRetry(ctx, opts, req)
	if err != nil {
		return &BrokenLink{
			URL:        linkURL,
			StatusCode: 0,
			Error:      err.Error(),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return &BrokenLink{
			URL:        linkURL,
			StatusCode: resp.StatusCode,
			Error:      fmt.Sprintf("HTTP status: %d", resp.StatusCode),
		}
	}

	return nil
}

func extractSEOFromDoc(doc *goquery.Document) SEO {
	seo := SEO{
		HasTitle:       false,
		Title:          "",
		HasDescription: false,
		Description:    "",
		HasH1:          false,
		H1:             "",
	}

	doc.Find("title").Each(func(i int, s *goquery.Selection) {
		title := s.Text()
		if title != "" {
			seo.HasTitle = true
			seo.Title = decodeHTMLEntities(title)
		}
	})

	doc.Find("meta[name='description']").Each(func(i int, s *goquery.Selection) {
		description, exists := s.Attr("content")
		if exists && description != "" {
			seo.HasDescription = true
			seo.Description = decodeHTMLEntities(description)
		}
	})

	doc.Find("h1").Each(func(i int, s *goquery.Selection) {
		if i == 0 {
			h1 := s.Text()
			if h1 != "" {
				seo.HasH1 = true
				seo.H1 = decodeHTMLEntities(h1)
			}
		}
	})

	return seo
}

func decodeHTMLEntities(text string) string {
	return html.UnescapeString(strings.TrimSpace(text))
}

func extractLinksFromHTML(body []byte, baseURL string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	links := make([]string, 0)
	seen := make(map[string]bool)

	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					href, err := url.Parse(attr.Val)
					if err != nil {
						continue
					}

					absURL := base.ResolveReference(href)

					if absURL.Scheme == "http" || absURL.Scheme == "https" {
						absURL.Fragment = ""
						urlStr := absURL.String()

						if !seen[urlStr] {
							seen[urlStr] = true
							links = append(links, urlStr)
						}
					}
					break
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)

	return links, nil
}
