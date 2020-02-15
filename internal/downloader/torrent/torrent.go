// Package torrent is a downloader implementer for downloading torrents
package torrent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	"github.com/pkg/errors"
)

type Client struct {
	config *torrent.ClientConfig
}

// NewClient creates a new torrent client
func NewClient() *Client {
	clientConfig := torrent.NewDefaultClientConfig()

	return &Client{
		config: clientConfig,
	}
}

// TODO(jaredallard): download currently stores everything in memory, we need to not do that
func (c *Client) Download(ctx context.Context, torrentURL, location string) error {
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
	if u.Scheme == "magnet:" {
		var err error
		t, err = client.AddMagnet(torrentURL)
		if err != nil {
			return errors.Wrap(err, "failed to add torrent")
		}
	} else {
		return fmt.Errorf("unsupported scheme '%s'", u.Scheme)
	}

	// TODO(jaredallard): add a timeout condition for stuck magnet URLs
	<-t.GotInfo()

	t.DownloadAll()

	// wait for the torrent to finish downloading
	if !client.WaitAll() {
		return fmt.Errorf("failed to download torrents")
	}

	for _, file := range t.Files() {
		dir := filepath.Dir(file.Path())
		name := filepath.Base(file.Path())

		saveDir := filepath.Join(location, dir)
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			return errors.Wrap(err, "failed to mkdir")
		}

		// open output file
		fo, err := os.Create(filepath.Join(saveDir, name))
		if err != nil {
			return err
		}

		// make a write buffer
		w := bufio.NewWriter(fo)

		// open the torrent reader
		r := file.NewReader()

		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if err != nil && err != io.EOF {
				return err
			}
			if n == 0 {
				break
			}

			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
		}

		// flush it to disk
		if err = w.Flush(); err != nil {
			return err
		}

		// close the torrent file
		if err = fo.Close(); err != nil {
			return err
		}
	}

	// we're done :tada:
	return nil
}
