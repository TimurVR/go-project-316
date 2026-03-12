package main

import (
	code "code/crawler"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:      "hexlet-go-crawler",
		Usage:     "analyze a website structure",
		UsageText: "hexlet-go-crawler [global options] <url>",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:     "depth",
				Value:    10,
				Usage:    "crawl depth",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.IntFlag{
				Name:     "retries",
				Value:    1,
				Usage:    "number of retries for failed requests",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.DurationFlag{
				Name:     "delay",
				Value:    0,
				Usage:    "delay between requests (example: 200ms, 1s)",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.DurationFlag{
				Name:     "timeout",
				Value:    15 * time.Second,
				Usage:    "per-request timeout (default: 15s)",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.FloatFlag{
				Name:     "rps",
				Value:    0,
				Usage:    "limit requests per second (overrides delay)",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.StringFlag{
				Name:     "user-agent",
				Value:    "",
				Usage:    "custom user agent",
				Category: "GLOBAL OPTIONS:",
			},
			&cli.IntFlag{
				Name:     "workers",
				Value:    4,
				Usage:    "number of concurrent workers",
				Category: "GLOBAL OPTIONS:",
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.NArg() == 0 {
				return cli.Exit("URL обязателен для анализа\nИспользование: hexlet-go-crawler [опции] <url>", 1)
			}
			url := c.Args().First()
			opts := code.Options{
				URL:         url,
				Depth:       c.Int("depth"),
				MaxRetries:     c.Int("retries"),
				Delay:       c.Duration("delay"),
				Timeout:     c.Duration("timeout"),
				RPS:         c.Float("rps"),
				UserAgent:   c.String("user-agent"),
				Concurrency: c.Int("workers"),
				IndentJSON:  true,
				HTTPClient:  nil,
			}
			res, err := code.Analyze(ctx, opts)
			if err != nil {
				return fmt.Errorf("error analyzing website: %w", err)
			}
			fmt.Print(string(res))
			return nil
		},
	}
	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
