package crawler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RateLimiter struct {
	LastRequest time.Time
	MinInterval time.Duration
	Mu          sync.Mutex
}

func GetHTMLWithContext(ctx context.Context, urlStr string, client *http.Client, ua string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", 404, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 404, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode, nil
}

func FetchAsset(ctx context.Context, client *http.Client, urlStr string, ua string) Asset {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return Asset{
			URL:        urlStr,
			Type:       "other",
			StatusCode: 0,
			SizeBytes:  0,
		}
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}

	resp, err := client.Do(req)
	if err != nil {
		return Asset{
			URL:        urlStr,
			Type:       "other",
			StatusCode: 0,
			SizeBytes:  0,
		}
	}
	defer func() { _ = resp.Body.Close() }()
	assetType := "other"
	lowerURL := strings.ToLower(urlStr)

	if strings.Contains(lowerURL, ".js") {
		assetType = "script"
	} else if strings.Contains(lowerURL, ".css") {
		assetType = "style"
	} else if strings.Contains(lowerURL, ".png") ||
		strings.Contains(lowerURL, ".jpg") ||
		strings.Contains(lowerURL, ".jpeg") ||
		strings.Contains(lowerURL, ".gif") ||
		strings.Contains(lowerURL, ".svg") ||
		strings.Contains(lowerURL, ".webp") ||
		strings.Contains(lowerURL, ".ico") {
		assetType = "image"
	}
	var size int64 = 0
	if resp.ContentLength > 0 {
		size = resp.ContentLength
	} else {
		body, err := io.ReadAll(resp.Body)
		if err == nil {
			size = int64(len(body))
		}
	}

	return Asset{
		URL:        urlStr,
		Type:       assetType,
		StatusCode: resp.StatusCode,
		SizeBytes:  size,
	}
}

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
		MinInterval: minInterval,
		LastRequest: time.Now().Add(-minInterval),
	}
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
	if rl == nil {
		return nil
	}
	rl.Mu.Lock()
	defer rl.Mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(rl.LastRequest)
	if elapsed < rl.MinInterval {
		waitTime := rl.MinInterval - elapsed
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
	rl.LastRequest = time.Now()
	return nil
}
