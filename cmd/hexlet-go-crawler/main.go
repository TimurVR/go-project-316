package main

import (
	"context"
	"fmt"
	code "hexlet-go-crawler/code/crawler"
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
				Name:  "depth",
				Value: 10,
				Usage: "crawl depth",
			},
			&cli.IntFlag{
				Name:  "retries",
				Value: 1,
				Usage: "number of retries for failed requests",
			},
			&cli.DurationFlag{
				Name:  "delay",
				Value: 0,
				Usage: "delay between requests (example: 200ms, 1s)",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Value: 15,
				Usage: "per-request timeout in seconds",
			},
			&cli.StringFlag{
				Name:  "user-agent",
				Value: "HexletGoCrawler/1.0",
				Usage: "custom user agent",
			},
			&cli.IntFlag{
				Name:  "workers",
				Value: 4,
				Usage: "number of concurrent workers",
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
				Retries:     c.Int("retries"),
				Delay:       c.Duration("delay"),
				Timeout:     c.Duration("timeout") * time.Second,
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
