package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/minio/minio-go/v6"
	log "github.com/sirupsen/logrus"
	"github.com/tritonmedia/downloader-go/internal/rabbitmq"
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
			log.Errorf("failed to recieve message: %v", err)
		}
	}()

	// TODO(jaredallard): we might want to be able to add more goroutines for this, but I
	// need to learn more about the scheduling system first
	go func() {
		for msg := range msgs {
			fmt.Println(msg)
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

	<-ctx.Done()

	// wait for shutdown
	log.Info("finished shutdown")
}
