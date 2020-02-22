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

type Client struct {
	config *torrent.ClientConfig
}

// NewClient creates a new torrent client
func NewClient(baseDir string) *Client {
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.DefaultStorage = storage.NewFile(baseDir)
	return &Client{
		config: clientConfig,
	}
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
func (c *Client) Download(ctx context.Context, progress chan downloader.ProgressUpdate, torrentURL string) error {
	// create a new client everytime to prevent state leakage
	client, err := torrent.NewClient(c.config)
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

	// TODO(jaredallard): add a timeout condition for stuck magnet URLs
	log.Infof("fetching torrent metadata")
	<-t.GotInfo()
	log.Infof("fetched torrent metadata")

	// download all files in the torrent
	t.DownloadAll()

	// publish the progress every 1 second
	stopChan := make(chan bool)
	progressReporter := time.NewTicker(1 * time.Second)
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
				progressReporter.Stop()
				return
			}
		}
	}()

	// wait for the torrent to finish downloading
	log.Infof("waiting for torrent download")
	if !client.WaitAll() {
		return fmt.Errorf("failed to download torrents")
	}

	close(stopChan)

	// we're done :tada:
	return nil
}
