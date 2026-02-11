package code

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

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

type Page struct {
	URL        string `json:"url"`
	Depth      int    `json:"depth"`
	HTTPStatus int    `json:"http_status"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
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
	report := Report{
		RootURL:     opts.URL,
		Depth:       opts.Depth,
		GeneratedAt: time.Now().UTC(),
		Pages:       make([]Page, 0),
	}
	req, err := http.NewRequestWithContext(ctx, "GET", opts.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("Ошибка при создании запроса: %w", err)
	}
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	} else {
		req.Header.Set("User-Agent", "HexletGoCrawler/1.0")
	}
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		report.Pages = append(report.Pages, Page{
			URL:        opts.URL,
			Depth:      0,
			HTTPStatus: 0,
			Status:     "error",
			Error:      err.Error(),
		})
	} else {
		defer resp.Body.Close()
		report.Pages = append(report.Pages, Page{
			URL:        opts.URL,
			Depth:      0,
			HTTPStatus: resp.StatusCode,
			Status:     "ok",
			Error:      "",
		})
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
