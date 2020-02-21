package downloader

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	log "github.com/sirupsen/logrus"
)

// ClientImpl is a downloader implementation
type ClientImpl interface {
	// Download downloads a file into a specific location
	Download(ctx context.Context, url string) error

	// Register is used for the client, and returns what protocols / file extensions
	// this downloader supports
	Register() ClientRegister
}

type ClientRegister struct {
	Name           string
	Protocols      []string
	FileExtensions []string
}

type Client struct {
	// these are arrays to allow chaining downloaders in case one wants
	// to pass, but this isn't implemented at the moment
	protocolImpl map[string][]ClientImpl
	fileExtsImpl map[string][]ClientImpl
}

// NewClient creates a new client
func NewClient(impls []ClientImpl) *Client {
	c := &Client{
		protocolImpl: make(map[string][]ClientImpl),
		fileExtsImpl: make(map[string][]ClientImpl),
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

	log.Infof("have %d protocol(s), and %d file extension(s) registered", len(c.protocolImpl), len(c.fileExtsImpl))

	return c
}

// Download downloads media into a given location
func (c *Client) Download(ctx context.Context, furl string) error {
	u, err := url.Parse(furl)
	if err != nil {
		return err
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
			downloader = c.protocolImpl[u.Scheme][0]
		}
	}

	// if it's still nil here, we didn't find a downloader
	if downloader == nil {
		return fmt.Errorf("unsupported fileext '%s' or protocol '%s'", fileext, u.Scheme)
	}

	return downloader.Download(ctx, furl)
}
