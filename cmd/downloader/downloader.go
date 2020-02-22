package main

import (
	"context"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gogo/protobuf/proto"
	"github.com/minio/minio-go/v6"
	log "github.com/sirupsen/logrus"
	"github.com/tritonmedia/downloader-go/internal/downloader"
	"github.com/tritonmedia/downloader-go/internal/downloader/torrent"
	"github.com/tritonmedia/downloader-go/internal/rabbitmq"
	api "github.com/tritonmedia/tritonmedia.go/pkg/proto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		log.SetReportCaller(true)
	}

	logFormat := strings.ToLower(os.Getenv("LOG_FORMAT"))
	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	}

	amqpEndpoint := os.Getenv("RABBITMQ_ENDPOINT")
	if amqpEndpoint == "" {
		amqpEndpoint = "127.0.0.1:5672"
		log.Warnf("RABBITMQ_ENDPOINT not defined, defaulting to local config: %s", amqpEndpoint)
	}

	log.Infoln("connecting to rabbitmq ...")
	client, err := rabbitmq.NewClient(ctx, amqpEndpoint)
	client.SetPrefetch(1)
	if err != nil {
		log.Fatalf("failed to connect to rabbitmq: %v", err)
	}
	log.Infoln("connected")

	var ssl bool
	endpoint := os.Getenv("S3_ENDPOINT")
	u, err := url.Parse(endpoint)
	if err != nil {
		log.Fatalf("failed to parse minio endpoint as a URL: %v", err)
	}

	if u.Scheme == "https" {
		log.Infof("minio: enabled TLS")
		ssl = true
	}

	m, err := minio.New(
		u.Host,
		os.Getenv("S3_ACCESS_KEY"),
		os.Getenv("S3_SECRET_KEY"),
		ssl,
	)
	if err != nil {
		log.Fatalf("failed to create minio (s3) client: %v", err)
	}

	msgs, errChan, err := client.Consume("v1.download")
	if err != nil {
		log.Fatalf("failed to consume from queues: %v", err)
	}

	// for now
	_ = m

	// this is a bad pattern, but since we're just looking to bubble up errors
	// I'm ok with this
	go func() {
		for err := range errChan {
			log.Errorf("failed to receive message: %v", err)
		}
	}()

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	downloader := downloader.NewClient(ctx, []downloader.ClientImpl{torrent.NewClient(filepath.Join(wd, "downloading"))})

	// TODO(jaredallard): we might want to be able to add more goroutines for this, but I
	// need to learn more about the scheduling system first
	go func() {
		for msg := range msgs {
			var job api.Download
			if err := proto.Unmarshal(msg.Delivery.Body, &job); err != nil {
				log.WithField("event", "decode-message").Errorf("failed to unmarshal rabbitmq message into protobuf format: %v", err)
				if err := msg.Nack(); err != nil {
					log.Warnf("failed to nack failed message: %v", err)
				}
				continue
			}

			log.WithField("job", job).Infof("got message")
			if job.Media.Source == api.Media_TORRENT {
				log.Info("started torrent downloader")
				if err := downloader.Download(ctx, job.Media.SourceURI); err != nil {
					log.Errorf("failed to download torrent: %v", err)
					continue
				}
			}
			log.Info("finished download")

		}
	}()

	// listen for interrupts and gracefully shutdown server
	c := make(chan os.Signal, 10)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-c
		log.Info("shutting down")
		cancel()
	}()

	// wait for context to be signified as done
	<-ctx.Done()

	// wait for the message processor to stop
	<-msgs

	// wait for shutdown
	log.Info("finished shutdown")
}
