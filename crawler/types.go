package crawler

import (
	"net/http"
	"sync"
	"time"
)

type Options struct {
	URL         string
	Depth       int
	Retries     int
	Delay       time.Duration
	Timeout     time.Duration
	RPS         int
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

type Asset struct {
	URL        string `json:"url"`
	Type       string `json:"type"`
	StatusCode int    `json:"status_code"`
	SizeBytes  int64  `json:"size_bytes"`
	Error      string `json:"error"`
}

type SEO struct {
	HasTitle       bool   `json:"has_title"`
	Title          string `json:"title"`
	HasDescription bool   `json:"has_description"`
	Description    string `json:"description"`
	HasH1          bool   `json:"has_h1"`
	H1             string `json:"h1"`
}
type Page struct {
	URL          string       `json:"url"`
	Depth        int          `json:"depth"`
	HTTPStatus   int          `json:"http_status"`
	Status       string       `json:"status"`
	Error        string       `json:"error"`
	BrokenLinks  []BrokenLink `json:"broken_links"`
	Assets       []Asset      `json:"assets"`
	SEO          SEO          `json:"seo"`
	DiscoveredAt time.Time    `json:"discovered_at"`
}

type Report struct {
	RootURL     string    `json:"root_url"`
	Depth       int       `json:"depth"`
	GeneratedAt time.Time `json:"generated_at"`
	Pages       []Page    `json:"pages"`
}

type assetCache struct {
	mu     sync.RWMutex
	assets map[string]*Asset
}

type crawlTask struct {
	url   string
	depth int
}
