package parser

import (
	types "code/internal/types"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func ExtractSEO(html string) *types.SEO {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return &types.SEO{}
	}
	seo := &types.SEO{}
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

func ExtractLinks(html string) []string {
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
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	var assets []string
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok && src != "" {
			assets = append(assets, src)
		}
	})
	doc.Find("script").Each(func(i int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok && src != "" {
			assets = append(assets, src)
		}
	})
	doc.Find("link[rel='stylesheet']").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok && href != "" {
			assets = append(assets, href)
		}
	})

	return assets
}
