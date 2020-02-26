// Package torrent is a downloader implementer for downloading torrents
package torrent

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/tritonmedia/downloader-go/internal/downloader"
)

// Client is a Torrent downloader
type Client struct{}

// NewClient creates a new torrent client
func NewClient() *Client {
	return &Client{}
}

// Register is called to register this implementation for a protocol
func (c *Client) Register() downloader.ClientRegister {
	return downloader.ClientRegister{
		Name: "torrent",
		Protocols: []string{
			"magnet",
		},
		FileExtensions: []string{
			".torrent",
		},
	}
}

// Download downloads a torrent
func (c *Client) Download(ctx context.Context, baseDir string, progress chan downloader.ProgressUpdate, torrentURL string) error {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DefaultStorage = storage.NewFile(baseDir)

	// create a new client everytime to prevent state leakage
	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		return err
	}
	defer client.Close()

	u, err := url.Parse(torrentURL)
	if err != nil {
		return err
	}

	var t *torrent.Torrent
	if u.Scheme == "magnet" {
		var err error
		t, err = client.AddMagnet(torrentURL)
		if err != nil {
			return errors.Wrap(err, "failed to add torrent")
		}
	} else {
		return fmt.Errorf("unsupported scheme '%s'", u.Scheme)
	}

	log.Infof("fetching torrent metadata")
	timeout := time.After(10 * time.Minute)
	select {
	case <-t.GotInfo():
		log.Infof("fetched torrent metadata")
	case <-timeout:
		return fmt.Errorf("failed to get metadata")
	case <-ctx.Done():
		// we're dying instead
		return ctx.Err()
	}

	// download all files in the torrent
	t.DownloadAll()

	// publish the progress every 1 second
	stopChan := make(chan bool)
	progressReporter := time.NewTicker(1 * time.Second)

	defer progressReporter.Stop()
	go func() {
		for {
			select {
			case <-progressReporter.C:
				completed := uint64(t.BytesCompleted())
				total := uint64(t.Info().TotalLength())
				progress <- downloader.ProgressUpdate{
					Progress: float64(completed) / float64(total) * 100,
					URL:      torrentURL,
				}
			case <-stopChan:
				log.Info("stopping progress reporter")
				return
			}
		}
	}()

	// wait for the torrent to finish downloading
	// TODO(jaredallard): extend context cancellation into here
	log.Infof("waiting for torrent download")
	if !client.WaitAll() {
		return fmt.Errorf("failed to download torrents")
	}

	progress <- downloader.ProgressUpdate{
		Progress: 100,
		URL:      torrentURL,
	}

	close(stopChan)

	// we're done :tada:
	return nil
}
