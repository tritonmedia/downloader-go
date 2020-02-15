package downloader

import "context"

type Client interface {
	// Download downloads a file into a specific location
	Download(ctx context.Context, url, location string) error
}
