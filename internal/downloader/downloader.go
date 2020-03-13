package downloader

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"time"

	log "github.com/sirupsen/logrus"
)

// ClientImpl is a downloader implementation
type ClientImpl interface {
	// Download downloads a file into a specific location
	Download(ctx context.Context, baseDir string, progress chan ProgressUpdate, url string) error

	// Register is used for the client, and returns what protocols / file extensions
	// this downloader supports
	Register() ClientRegister
}

// ClientRegister is a struct used to register a downloader implementation with a given client
type ClientRegister struct {
	// Name is the name of this client, e.g. Downloade
	Name string

	// Protocols is a []string of the url.Schemes supported by this client, and will
	// trigger the invocation of the client.Download method.
	Protocols []string

	// FileExtensions is the list of file extensions supported by this downloader
	// and results in this downloader being called -- regardless of url.Scheme, however
	// this will only trigger on http[s] URLs
	FileExtensions []string
}

// Client is a downloader client
type Client struct {
	// these are arrays to allow chaining downloaders in case one wants
	// to pass, but this isn't implemented at the moment
	protocolImpl map[string][]ClientImpl
	fileExtsImpl map[string][]ClientImpl

	// baseDir to download all things into, will follow format like such:
	// baseDir/jobId/...
	baseDir string

	// used to store, and track, the progress of downloads
	progress        map[string]float64
	progressUpdates chan ProgressUpdate
}

// ProgressUpdate is emitted when the progress of a download updates, this is sent from
// a ClientImpl to the client
type ProgressUpdate struct {
	URL      string
	Progress float64
}

// NewClient creates a new client
func NewClient(ctx context.Context, baseDir string, impls []ClientImpl) (*Client, error) {
	c := &Client{
		protocolImpl:    make(map[string][]ClientImpl),
		baseDir:         baseDir,
		fileExtsImpl:    make(map[string][]ClientImpl),
		progress:        make(map[string]float64),
		progressUpdates: make(chan ProgressUpdate),
	}

	if c.baseDir == "" || !filepath.IsAbs(c.baseDir) {
		return nil, fmt.Errorf("invalid baseDir")
	}

	for _, impl := range impls {
		reg := impl.Register()

		log.WithFields(log.Fields{
			"name":     reg.Name,
			"exts":     reg.FileExtensions,
			"protocol": reg.Protocols,
		}).Infof("registered client implementation")

		for _, fileext := range reg.FileExtensions {
			c.fileExtsImpl[fileext] = append(c.fileExtsImpl[fileext], impl)
		}

		for _, protocol := range reg.Protocols {
			c.protocolImpl[protocol] = append(c.protocolImpl[protocol], impl)
		}
	}

	// progress update consumer
	go func() {
		for {
			select {
			case p := <-c.progressUpdates:
				// remove an item once it hits 100
				if p.Progress == 100 {
					delete(c.progress, p.URL)
					continue
				}

				c.progress[p.URL] = p.Progress
			case <-ctx.Done():
				close(c.progressUpdates)
				return
			}
		}
	}()

	// progress display consumer
	updateTicker := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-updateTicker.C:
				for url, progress := range c.progress {
					log.
						WithFields(log.Fields{"progress": math.Ceil(progress*100) / 100, "url": url}).
						Infof("download status")
				}
			case <-ctx.Done():
				updateTicker.Stop()
				return
			}
		}
	}()

	log.Infof("have %d protocol(s), and %d file extension(s) registered", len(c.protocolImpl), len(c.fileExtsImpl))

	return c, nil
}

// Download downloads media into a given location
func (c *Client) Download(ctx context.Context, id string, furl string) (string, error) {
	u, err := url.Parse(furl)
	if err != nil {
		return "", err
	}
	fileext := filepath.Ext(u.Path)

	log.WithFields(log.Fields{"protocol": u.Scheme, "ext": fileext}).Infof("downloading file")

	var downloader ClientImpl
	// only process fileext when it's a http/s protocol
	if u.Scheme == "http" || u.Scheme == "https" {
		if len(c.fileExtsImpl[fileext]) > 0 {
			downloader = c.fileExtsImpl[fileext][0]
		}
	}

	// only check if we didn't find a downloader in the above step
	if downloader == nil {
		if len(c.protocolImpl[u.Scheme]) > 0 {
			log.Infof("found supported protocol downloader")

			// HACK: calls the first supported protocol downloader
			downloader = c.protocolImpl[u.Scheme][0]
		}
	}

	// if it's still nil here, we didn't find a downloader
	if downloader == nil {
		return "", fmt.Errorf("unsupported fileext '%s' or protocol '%s'", fileext, u.Scheme)
	}

	baseDir := filepath.Join(c.baseDir, id)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", err
	}

	return baseDir, downloader.Download(ctx, baseDir, c.progressUpdates, furl)
}
