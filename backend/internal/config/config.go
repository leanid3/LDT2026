// Package config загружает конфигурацию сервисов (config.yaml + переопределение через ENV).
//
// Секреты (пароли, ключи) — только через ENV, в репозиторий кладём только config.example.yaml
// (backend-plan.md §2: "config.yaml с паролями в репозитории" — исправлено).
package config

import (
	"log/slog"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	Logger   LoggerConfig   `yaml:"logger"`
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Broker   BrokerConfig   `yaml:"broker"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Minio    MinioConfig    `yaml:"minio"`
}

type LoggerConfig struct {
	Level string `yaml:"level" env:"LOGGER_LEVEL" env-default:"info"` // debug|info|warn|error
}

type ServerConfig struct {
	Port            int           `yaml:"port" env:"SERVER_PORT" env-default:"8080"`
	Host            string        `yaml:"host" env:"SERVER_HOST" env-default:"0.0.0.0"`
	ReadTimeout     time.Duration `yaml:"read_timeout" env:"SERVER_READ_TIMEOUT" env-default:"10s"`
	WriteTimeout    time.Duration `yaml:"write_timeout" env:"SERVER_WRITE_TIMEOUT" env-default:"10s"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SERVER_SHUTDOWN_TIMEOUT" env-default:"5s"`
}

type DatabaseConfig struct {
	Host            string        `yaml:"host" env:"DATABASE_HOST"`
	Port            int           `yaml:"port" env:"DATABASE_PORT" env-default:"5432"`
	User            string        `yaml:"user" env:"DATABASE_USER"`
	Password        string        `yaml:"password" env:"DATABASE_PASSWORD"`
	Database        string        `yaml:"database" env:"DATABASE_DATABASE"`
	MaxConnections  int32         `yaml:"max_connections" env:"DATABASE_MAX_CONNECTIONS" env-default:"25"`
	MinConnections  int32         `yaml:"min_connections" env:"DATABASE_MIN_CONNECTIONS" env-default:"5"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime" env:"DATABASE_MAX_CONN_LIFETIME" env-default:"1h"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time" env:"DATABASE_MAX_CONN_IDLE_TIME" env-default:"30m"`
}

// BrokerConfig — Kafka. Список топиков см. backend-plan.md §6.2 (doc.parse.requested, doc.parse.completed,
// doc.extract.requested, doc.extract.completed, protocol.ready, rin.sync.requested + *.dlq); сами
// producer/consumer подключаются к доменным сервисам начиная с M1/M3, на M0 конфиг только читается.
type BrokerConfig struct {
	BootstrapServers string `yaml:"bootstrap_servers" env:"BROKER_BOOTSTRAP_SERVERS"`
	ClientID         string `yaml:"client_id" env:"BROKER_CLIENT_ID"`

	ProducerAcks              string `yaml:"producer_acks" env:"BROKER_PRODUCER_ACKS" env-default:"-1"`
	ProducerEnableIdempotence bool   `yaml:"producer_idempotence" env:"BROKER_PRODUCER_IDEMPOTENCE" env-default:"true"`
	ProducerCompression       string `yaml:"producer_compression" env:"BROKER_PRODUCER_COMPRESSION" env-default:"snappy"`
	ProducerRetries           int    `yaml:"producer_retries" env:"BROKER_PRODUCER_RETRIES" env-default:"3"`

	ConsumerSessionTimeoutMs    int `yaml:"consumer_session_timeout_ms" env:"BROKER_CONSUMER_SESSION_TIMEOUT_MS" env-default:"30000"`
	ConsumerHeartbeatIntervalMs int `yaml:"consumer_heartbeat_interval_ms" env:"BROKER_CONSUMER_HEARTBEAT_INTERVAL_MS" env-default:"10000"`
	ConsumerMaxPollIntervalMs   int `yaml:"consumer_max_poll_interval_ms" env:"BROKER_CONSUMER_MAX_POLL_INTERVAL_MS" env-default:"1800000"`
	ConsumerFetchMaxBytes       int `yaml:"consumer_fetch_max_bytes" env:"BROKER_CONSUMER_FETCH_MAX_BYTES" env-default:"52428800"`

	SecurityProtocol string `yaml:"security_protocol" env:"BROKER_SECURITY_PROTOCOL" env-default:"PLAINTEXT"`
	SaslMechanism    string `yaml:"sasl_mechanism" env:"BROKER_SASL_MECHANISM"`
	SaslUsername     string `yaml:"sasl_username" env:"BROKER_SASL_USERNAME"`
	SaslPassword     string `yaml:"sasl_password" env:"BROKER_SASL_PASSWORD"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled" env:"METRICS_ENABLED" env-default:"true"`
	Port    int    `yaml:"port" env:"METRICS_PORT" env-default:"9090"`
	Path    string `yaml:"path" env:"METRICS_PATH" env-default:"/metrics"`
}

type MinioConfig struct {
	Endpoint  string        `yaml:"endpoint" env:"MINIO_ENDPOINT"`
	AccessKey string        `yaml:"access_key" env:"MINIO_ACCESS_KEY"`
	SecretKey string        `yaml:"secret_key" env:"MINIO_SECRET_KEY"`
	Bucket    string        `yaml:"bucket" env:"MINIO_BUCKET" env-default:"documents"`
	UseSSL    bool          `yaml:"use_ssl" env:"MINIO_USE_SSL" env-default:"false"`
	Region    string        `yaml:"region" env:"MINIO_REGION" env-default:"us-east-1"`
	Timeout   time.Duration `yaml:"timeout" env:"MINIO_TIMEOUT" env-default:"30s"`
}

// Load читает config.yaml (если есть) и переопределяет значениями из ENV; при отсутствии файла
// читает конфигурацию только из ENV.
func Load(path string) (*Config, error) {
	var cfg Config

	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, err
		}
		slog.Info("config loaded from environment variables only")
		return &cfg, nil
	}

	slog.Info("config loaded from file and environment variables", "path", path)
	return &cfg, nil
}
