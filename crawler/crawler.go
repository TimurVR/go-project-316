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
)

type crawlerApp struct {
	opts       Options
	visited    sync.Map
	assetCache sync.Map
	results    []PageReport
	mu         sync.Mutex
	limiter    *time.Ticker
}

func Analyze(ctx context.Context, opts Options) ([]byte, error) {
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}

	app := &crawlerApp{
		opts:    opts,
		results: make([]PageReport, 0),
	}

	if opts.RPS > 0 {
		app.limiter = time.NewTicker(time.Second / time.Duration(opts.RPS))
		defer app.limiter.Stop()
	}

	app.crawl(ctx, opts.URL, 0)

	report := Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       app.results,
	}

	if opts.IndentJSON {
		return json.MarshalIndent(report, "", "  ")
	}
	return json.Marshal(report)
}

func (c *crawlerApp) crawl(ctx context.Context, target string, depth int) {
	if depth > c.opts.Depth {
		return
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return
	}
	canon := parsed.String()

	if _, loaded := c.visited.LoadOrStore(canon, true); loaded {
		return
	}

	if c.limiter != nil {
		select {
		case <-c.limiter.C:
		case <-ctx.Done():
			return
		}
	}

	page := PageReport{
		URL:          canon,
		Depth:        depth,
		Status:       "ok",
		DiscoveredAt: time.Now().UTC(),
		BrokenLinks:  make([]BrokenLink, 0),
		Assets:       make([]Asset, 0),
	}

	resp, err := c.doRequest(ctx, canon)
	if err != nil {
		page.Status = "error"
		page.Error = err.Error()
		c.addResult(page)
		return
	}
	defer resp.Body.Close()

	page.HTTPStatus = resp.StatusCode
	doc, _ := goquery.NewDocumentFromReader(resp.Body)
	page.SEO = extractSEO(doc)

	internalLinks := c.processElements(ctx, doc, parsed, &page)
	c.addResult(page)

	for _, link := range internalLinks {
		c.crawl(ctx, link, depth+1)
	}
}

func (c *crawlerApp) doRequest(ctx context.Context, target string) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= c.opts.Retries; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
		if c.opts.UserAgent != "" {
			req.Header.Set("User-Agent", c.opts.UserAgent)
		}

		resp, err := c.opts.HTTPClient.Do(req)
		if err == nil {
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return resp, nil
			}
			lastErr = fmt.Errorf("status code: %d", resp.StatusCode)
			resp.Body.Close()
		} else {
			lastErr = err
		}

		if i < c.opts.Retries {
			select {
			case <-time.After(time.Duration(i+1) * 200 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

func (c *crawlerApp) addResult(p PageReport) {
	c.mu.Lock()
	c.results = append(c.results, p)
	c.mu.Unlock()
}

func extractSEO(doc *goquery.Document) SEOMetrics {
	seo := SEOMetrics{}
	if t := doc.Find("title").First(); t.Length() > 0 {
		seo.HasTitle = true
		seo.Title = strings.TrimSpace(t.Text())
	}
	if d := doc.Find("meta[name='description']").First(); d.Length() > 0 {
		val, _ := d.Attr("content")
		seo.HasDescription = true
		seo.Description = strings.TrimSpace(val)
	}
	if doc.Find("h1").Length() > 0 {
		seo.HasH1 = true
	}
	return seo
}

func (c *crawlerApp) processElements(ctx context.Context, doc *goquery.Document, base *url.URL, page *PageReport) []string {
	var internal []string
	doc.Find("img, script, link[rel='stylesheet']").Each(func(_ int, s *goquery.Selection) {
		attr, aType := "src", "image"
		if s.Is("link") {
			attr, aType = "href", "style"
		}
		if s.Is("script") {
			aType = "script"
		}

		val, _ := s.Attr(attr)
		if val == "" {
			return
		}
		abs := resolve(base, val)
		page.Assets = append(page.Assets, c.checkAsset(ctx, abs, aType))
	})

	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		abs := resolve(base, href)
		if abs == "" || strings.HasPrefix(abs, "mailto:") || strings.HasPrefix(abs, "tel:") {
			return
		}

		if isInternal(c.opts.URL, abs) {
			internal = append(internal, abs)
		} else {
			if bl := c.checkBroken(ctx, abs); bl != nil {
				page.BrokenLinks = append(page.BrokenLinks, *bl)
			}
		}
	})
	return internal
}

func resolve(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
}

func isInternal(root, target string) bool {
	r, _ := url.Parse(root)
	t, _ := url.Parse(target)
	return r.Host == t.Host
}

func (c *crawlerApp) checkAsset(ctx context.Context, target, aType string) Asset {
	if cached, ok := c.assetCache.Load(target); ok {
		return cached.(Asset)
	}
	asset := Asset{URL: target, Type: aType}
	resp, err := c.doRequest(ctx, target)
	if err != nil {
		asset.Error = err.Error()
	} else {
		asset.StatusCode = resp.StatusCode
		if resp.ContentLength > 0 {
			asset.SizeBytes = resp.ContentLength
		} else {
			b, _ := io.ReadAll(resp.Body)
			asset.SizeBytes = int64(len(b))
		}
		resp.Body.Close()
	}
	c.assetCache.Store(target, asset)
	return asset
}

func (c *crawlerApp) checkBroken(ctx context.Context, target string) *BrokenLink {
	resp, err := c.doRequest(ctx, target)
	if err != nil {
		return &BrokenLink{URL: target, Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &BrokenLink{URL: target, StatusCode: resp.StatusCode, Error: http.StatusText(resp.StatusCode)}
	}
	return nil
}
