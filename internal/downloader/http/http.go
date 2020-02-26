// Package http is a downloader implementer for downloading files from HTTP/S
package http

import (
	"context"
	"time"

	"github.com/cavaliercoder/grab"
	"github.com/tritonmedia/downloader-go/internal/downloader"
)

type Client struct {
	client *grab.Client
}

// NewClient creates a new http client
func NewClient() *Client {
	return &Client{
		client: grab.NewClient(),
	}
}

// Register is called to register this implementation for a protocol
func (c *Client) Register() downloader.ClientRegister {
	return downloader.ClientRegister{
		Name: "http",
		Protocols: []string{
			"http",
			"https",
		},
	}
}

// Download downloads a torrent
func (c *Client) Download(ctx context.Context, baseDir string, progress chan downloader.ProgressUpdate, furl string) error {
	req, err := grab.NewRequest(baseDir, furl)
	if err != nil {
		return err
	}

	resp := c.client.Do(req)

	// publish the progress every 1 second
	progressReporter := time.NewTicker(1 * time.Second)

	defer progressReporter.Stop()

LOOP:
	for {
		select {
		case <-progressReporter.C: // progress update due
			progress <- downloader.ProgressUpdate{
				Progress: resp.Progress(),
				URL:      furl,
			}
		case <-ctx.Done(): // program term
			break LOOP
		case <-resp.Done: // download finished
			break LOOP
		}
	}

	// we're done :tada:
	return nil
}
