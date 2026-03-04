package crawler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

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

func (c *crawlerApp) processPageElements(ctx context.Context, doc *goquery.Document, base *url.URL, page *PageReport) []string {
	var internalLinks []string
	doc.Find("img, script, link[rel='stylesheet']").Each(func(_ int, s *goquery.Selection) {
		attr := "src"
		aType := "image"
		if s.Is("link") {
			attr = "href"
			aType = "style"
		}
		if s.Is("script") {
			aType = "script"
		}

		val, _ := s.Attr(attr)
		if val == "" {
			return
		}
		abs := resolveURL(base, val)
		page.Assets = append(page.Assets, c.checkAsset(ctx, abs, aType))
	})
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		abs := resolveURL(base, href)
		if abs == "" || strings.HasPrefix(abs, "mailto:") {
			return
		}

		if isSameDomain(c.opts.URL, abs) {
			internalLinks = append(internalLinks, abs)
		} else {
			if bl := c.checkBroken(ctx, abs); bl != nil {
				page.BrokenLinks = append(page.BrokenLinks, *bl)
			}
		}
	})
	return internalLinks
}

func resolveURL(base *url.URL, ref string) string {
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
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
		asset.SizeBytes = resp.ContentLength
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
