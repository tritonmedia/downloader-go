package uploader

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"

	"github.com/minio/minio-go/v6"
	"github.com/minio/minio-go/v6/pkg/credentials"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// Client is an uploader client for uploading media
// to S3
type Client struct {
	bucket   string
	s3client *minio.Client
}

// NewUploader creates a new uploader client
func NewUploader(bucketName string) (*Client, error) {
	endpoint := os.Getenv("S3_ENDPOINT")

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse S3_ENDPOINT")
	}

	secure := false
	if u.Scheme == "https" {
		secure = true
	}

	endpoint = u.Hostname()
	if u.Port() != "" {
		endpoint = endpoint + ":" + u.Port()
	}

	// TODO(jaredallard): maybe add region support
	client, err := minio.NewWithOptions(endpoint, &minio.Options{
		Secure: secure,
		Creds: credentials.NewChainCredentials([]credentials.Provider{
			&EnvGeneric{},
			&credentials.EnvAWS{},
			&credentials.EnvMinio{},
		}),
		BucketLookup: minio.BucketLookupAuto,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create minio client")
	}
	return &Client{
		bucket:   bucketName,
		s3client: client,
	}, nil
}

// UploadFiles uploads a list of files, the path should be absolute
// TODO(jaredallard): add error handling and retries
func (c *Client) UploadFiles(ctx context.Context, mediaId string, baseDir string, files []string) error {
	if exists, err := c.s3client.BucketExists(c.bucket); err == nil && !exists {
		if err := c.s3client.MakeBucket(c.bucket, ""); err != nil {
			log.Warnf("failed to create bucket: %v", err)
		} else {
			log.Infof("created bucket")
		}
	}

	for _, fileName := range files {
		f, err := os.Open(fileName)
		if err != nil {
			log.Warnf("failed to open file: %v", err)
			continue
		}

		stats, err := os.Stat(fileName)
		if err != nil {
			log.Warnf("failed to stat file: %v", err)
			continue
		}

		// we base64 encode to prevent invalid characters from causing issues
		encodedName := base64.StdEncoding.EncodeToString([]byte(filepath.Base(fileName)))

		log.Infof("starting upload of file '%v'", encodedName)
		if _, err := c.s3client.PutObjectWithContext(ctx, c.bucket, filepath.Join(mediaId, "original/", encodedName), f, stats.Size(), minio.PutObjectOptions{}); err != nil {
			log.Errorf("failed to upload file: %v", err)
			continue
		}
		log.Infof("finished upload")
	}

	return nil
}
