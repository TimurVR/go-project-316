package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	fetcher "code/internal/fetcher"
	parser "code/internal/parser"
	types "code/internal/types"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	assetCache = make(map[string]types.AssetCacheItem)
	assetMu    sync.RWMutex
)

func Analyze(ctx context.Context, opts types.Options) ([]byte, error) {
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
	rateLimiter := fetcher.NewRateLimiter(opts.Delay, opts.RPS)
	report := types.Report{
		RootURL:     rawRoot,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       make([]types.Page, 0),
	}

	visited := make(map[string]bool)
	visitedMu := sync.Mutex{}
	pagesMu := sync.Mutex{}
	pagesMap := make(map[string]types.Page)

	taskChan := make(chan types.CrawlTask, 10000)
	var activeTasks sync.WaitGroup
	var workersWg sync.WaitGroup

	assetMu.Lock()
	assetCache = make(map[string]types.AssetCacheItem)
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
					html, err := fetcher.GetHTMLWithContext(ctx, page.URL, opts.HTTPClient, opts.UserAgent)
					if err == nil {
						baseURL, _ := url.Parse(page.URL)
						for _, link := range parser.ExtractLinks(html) {
							absLink, _ := NormalizeURL(link, baseURL)
							if absLink == "" {
								continue
							}

							linkURL, _ := url.Parse(absLink)
							if IsSameDomain(linkURL, rootURL) {
								visitedMu.Lock()
								if !visited[absLink] {
									visited[absLink] = true
									if task.Depth+1 < opts.Depth {
										activeTasks.Add(1)
										select {
										case taskChan <- types.CrawlTask{URL: absLink, Depth: task.Depth + 1}:
										case <-ctx.Done():
											activeTasks.Done()
										}
									}
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
	taskChan <- types.CrawlTask{URL: rawRoot, Depth: 0}

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

func crawlPage(ctx context.Context, opts types.Options, pageURL string, depth int) (types.Page, error) {
	page := types.Page{
		URL:          pageURL,
		Depth:        depth,
		DiscoveredAt: time.Now().UTC(),
		SEO:          &types.SEO{},
	}

	html, err := fetcher.GetHTMLWithContext(ctx, pageURL, opts.HTTPClient, opts.UserAgent)
	if err != nil {
		page.Status = "error"
		page.Error = err.Error()
		page.Assets = nil
		page.BrokenLinks = nil
		return page, nil
	}

	page.SEO = parser.ExtractSEO(html)
	page.Status = "ok"
	page.HTTPStatus = 200
	page.Assets = make([]types.Asset, 0)
	page.BrokenLinks = make([]types.BrokenLink, 0)

	baseURL, _ := url.Parse(pageURL)
	assetURLs := parser.ExtractAssetURLs(html)
	for _, assetURL := range assetURLs {
		abs, _ := NormalizeURL(assetURL, baseURL)
		if !ShouldCheckAsset(abs) {
			continue
		}
		assetMu.RLock()
		cached, exists := assetCache[abs]
		assetMu.RUnlock()

		if exists {
			page.Assets = append(page.Assets, cached.Asset)
			continue
		}
		asset := fetcher.FetchAsset(ctx, opts.HTTPClient, abs, opts.UserAgent)
		assetMu.Lock()
		assetCache[abs] = types.AssetCacheItem{Asset: asset}
		assetMu.Unlock()
		page.Assets = append(page.Assets, asset)
	}
	linkURLs := parser.ExtractLinks(html)
	for _, link := range linkURLs {
		absLink, _ := NormalizeURL(link, baseURL)
		if absLink == "" {
			continue
		}
		if !strings.HasPrefix(absLink, "http") {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, "HEAD", absLink, nil)
		if err != nil {
			continue
		}
		if opts.UserAgent != "" {
			req.Header.Set("User-Agent", opts.UserAgent)
		}

		resp, err := opts.HTTPClient.Do(req)
		if err != nil {
			page.BrokenLinks = append(page.BrokenLinks, types.BrokenLink{
				URL:        absLink,
				StatusCode: 0,
				Error:      err.Error(),
			})
			continue
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode >= 400 {
			page.BrokenLinks = append(page.BrokenLinks, types.BrokenLink{
				URL:        absLink,
				StatusCode: resp.StatusCode,
				Error:      fmt.Sprintf("HTTP %d", resp.StatusCode),
			})
		}
	}

	return page, nil
}
