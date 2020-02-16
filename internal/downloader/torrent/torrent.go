// Package torrent is a downloader implementer for downloading torrents
package torrent

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/storage"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
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

func (c *Client) Download(ctx context.Context, torrentURL string) error {

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

	// run a progress reporter that emits the status every 5 seconds to
	// the standard logger
	stopChan := make(chan bool)
	progressReporter := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-progressReporter.C:
				completed := uint64(t.BytesCompleted())
				total := uint64(t.Info().TotalLength())
				log.
					WithField("progress", math.Ceil((float64(completed)/float64(total)*100)*100)/100).
					Infof("torrent status (%s/%s)", humanize.Bytes(completed), humanize.Bytes(total))
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
