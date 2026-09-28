// Package minio оборачивает minio-go/v7: подключение и создание бакета при старте.
package minio

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Prefix    string
	UseSSL    bool
	Region    string
	Timeout   time.Duration

	// PublicEndpoint — адрес MinIO, каким его видит БРАУЗЕР (presigned-ссылки подписываются под конкретный
	// Host, подменить его после подписи нельзя). Пусто — совпадает с Endpoint (backend на хосте рядом с
	// MinIO). В контейнерах Endpoint = "minio:9000" (внутренняя сеть), а браузеру нужен опубликованный адрес.
	PublicEndpoint string
	PublicUseSSL   bool
}

type Client struct {
	client    *minio.Client
	presigner *minio.Client // подписывает ссылки для браузера; не ходит в сеть (регион задан)
	cfg       Config
	log       *slog.Logger
}

// New подключается к MinIO и гарантирует существование бакета.
func New(ctx context.Context, cfg Config, log *slog.Logger) (*Client, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	presigner := c
	if cfg.PublicEndpoint != "" && (cfg.PublicEndpoint != cfg.Endpoint || cfg.PublicUseSSL != cfg.UseSSL) {
		presigner, err = minio.New(cfg.PublicEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.PublicUseSSL,
			Region: cfg.Region, // с заданным регионом подпись считается локально, без запроса к MinIO
		})
		if err != nil {
			return nil, fmt.Errorf("create minio presign client: %w", err)
		}
	}

	client := &Client{client: c, presigner: presigner, cfg: cfg, log: log}
	if err := client.ensureBucket(ctx); err != nil {
		return nil, err
	}

	log.Info("minio connected", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket)
	return client, nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	exists, err := c.client.BucketExists(ctx, c.cfg.Bucket)
	if err != nil {
		return fmt.Errorf("check bucket exists: %w", err)
	}

	if !exists {
		if err := c.client.MakeBucket(ctx, c.cfg.Bucket, minio.MakeBucketOptions{Region: c.cfg.Region}); err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
	}
	return nil
}

func (c *Client) Raw() *minio.Client {
	return c.client
}

// Presigner — клиент для presigned-ссылок, отдаваемых браузеру (POST на загрузку, GET на просмотр).
// Все остальные операции (чтение, запись, проверки) — через Raw().
func (c *Client) Presigner() *minio.Client {
	return c.presigner
}

func (c *Client) BucketName() string {
	return c.cfg.Bucket
}

// HealthCheck используется в /readyz.
func (c *Client) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := c.client.BucketExists(ctx, c.cfg.Bucket); err != nil {
		return fmt.Errorf("minio not responding: %w", err)
	}
	return nil
}
