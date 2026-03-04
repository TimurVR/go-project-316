package crawler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
	app := &crawlerApp{opts: opts}
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

func (c *crawlerApp) crawl(ctx context.Context, targetURL string, depth int) {
	if depth > c.opts.Depth { return }
	
	parsed, _ := url.Parse(targetURL)
	canon := parsed.String()
	if _, loaded := c.visited.LoadOrStore(canon, true); loaded { return }

	if c.limiter != nil {
		select {
		case <-c.limiter.C:
		case <-ctx.Done():
			return
		}
	}

	resp, err := c.doRequest(ctx, canon)
	page := PageReport{
		URL:          canon,
		Depth:        depth,
		Status:       "ok",
		DiscoveredAt: time.Now().UTC(),
		BrokenLinks:  []BrokenLink{},
		Assets:       []Asset{},
	}

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
	
	foundLinks := c.processPageElements(ctx, doc, parsed, &page)
	c.addResult(page)

	for _, link := range foundLinks {
		if isSameDomain(c.opts.URL, link) {
			c.crawl(ctx, link, depth+1)
		}
	}
}

func (c *crawlerApp) doRequest(ctx context.Context, target string) (*http.Response, error) {
	var lastErr error
	for i := 0; i <= c.opts.Retries; i++ {
		req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
		if c.opts.UserAgent != "" { req.Header.Set("User-Agent", c.opts.UserAgent) }
		
		resp, err := c.opts.HTTPClient.Do(req)
		if err == nil && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return resp, nil
		}
		lastErr = err
		if resp != nil { resp.Body.Close() }
		time.Sleep(time.Duration(i) * 500 * time.Millisecond)
	}
	return nil, lastErr
}

func (c *crawlerApp) addResult(p PageReport) {
	c.mu.Lock()
	c.results = append(c.results, p)
	c.mu.Unlock()
}

func isSameDomain(root, target string) bool {
	r, _ := url.Parse(root)
	t, _ := url.Parse(target)
	return r.Host == t.Host
}

