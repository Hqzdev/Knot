package config

import (
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/api"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
)

const (
	defaultMaxSize         = int64(100 << 20)
	maximumSize            = int64(5 << 30)
	defaultUploadTTL       = 15 * time.Minute
	defaultPendingTTL      = 24 * time.Hour
	defaultDownloadTTL     = 5 * time.Minute
	defaultCleanupInterval = time.Minute
	defaultCleanupBatch    = 100
)

type Config struct {
	Address         string
	JWTSecret       []byte
	MetadataMode    string
	DatabaseURL     string
	S3              objectstore.S3Config
	Limits          api.Limits
	CleanupInterval time.Duration
	CleanupBatch    int
}

func Load() (Config, error) {
	address := valueOrDefault("KNOT_ATTACHMENTS_ADDRESS", ":8082")
	secret := os.Getenv("KNOT_JWT_SECRET")
	if secret == "" {
		return Config{}, errors.New("KNOT_JWT_SECRET is required")
	}
	metadataMode := valueOrDefault("KNOT_ATTACHMENTS_STORE", "postgres")
	databaseURL := os.Getenv("KNOT_ATTACHMENTS_DATABASE_URL")
	if metadataMode != "postgres" && metadataMode != "memory" {
		return Config{}, errors.New("KNOT_ATTACHMENTS_STORE must be postgres or memory")
	}
	if metadataMode == "postgres" && databaseURL == "" {
		return Config{}, errors.New("KNOT_ATTACHMENTS_DATABASE_URL is required for postgres")
	}
	maxSize, err := boundedInt64("KNOT_ATTACHMENT_MAX_SIZE_BYTES", defaultMaxSize, 1, maximumSize)
	if err != nil {
		return Config{}, err
	}
	uploadTTL, err := boundedDuration("KNOT_ATTACHMENT_UPLOAD_TTL", defaultUploadTTL, time.Minute, time.Hour)
	if err != nil {
		return Config{}, err
	}
	pendingTTL, err := boundedDuration("KNOT_ATTACHMENT_PENDING_TTL", defaultPendingTTL, time.Hour, 7*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	if pendingTTL < uploadTTL {
		return Config{}, errors.New("KNOT_ATTACHMENT_PENDING_TTL must not be shorter than upload TTL")
	}
	downloadTTL, err := boundedDuration("KNOT_ATTACHMENT_DOWNLOAD_TTL", defaultDownloadTTL, time.Minute, time.Hour)
	if err != nil {
		return Config{}, err
	}
	cleanupInterval, err := boundedDuration("KNOT_ATTACHMENT_CLEANUP_INTERVAL", defaultCleanupInterval, 10*time.Second, time.Hour)
	if err != nil {
		return Config{}, err
	}
	cleanupBatchValue, err := boundedInt64("KNOT_ATTACHMENT_CLEANUP_BATCH", defaultCleanupBatch, 1, 1000)
	if err != nil {
		return Config{}, err
	}
	pathStyle, err := strconv.ParseBool(valueOrDefault("KNOT_S3_PATH_STYLE", "true"))
	if err != nil {
		return Config{}, errors.New("KNOT_S3_PATH_STYLE must be a boolean")
	}
	s3 := objectstore.S3Config{
		Endpoint:        os.Getenv("KNOT_S3_ENDPOINT"),
		PublicEndpoint:  os.Getenv("KNOT_S3_PUBLIC_ENDPOINT"),
		Region:          valueOrDefault("KNOT_S3_REGION", "us-east-1"),
		Bucket:          os.Getenv("KNOT_S3_BUCKET"),
		AccessKeyID:     os.Getenv("KNOT_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("KNOT_S3_SECRET_ACCESS_KEY"),
		SessionToken:    os.Getenv("KNOT_S3_SESSION_TOKEN"),
		PathStyle:       pathStyle,
	}
	if s3.Endpoint == "" || s3.Bucket == "" || s3.AccessKeyID == "" || s3.SecretAccessKey == "" {
		return Config{}, errors.New("S3 endpoint, bucket, access key ID, and secret access key are required")
	}
	return Config{
		Address:      address,
		JWTSecret:    []byte(secret),
		MetadataMode: metadataMode,
		DatabaseURL:  databaseURL,
		S3:           s3,
		Limits: api.Limits{
			MaxSize:     maxSize,
			UploadTTL:   uploadTTL,
			PendingTTL:  pendingTTL,
			DownloadTTL: downloadTTL,
		},
		CleanupInterval: cleanupInterval,
		CleanupBatch:    int(cleanupBatchValue),
	}, nil
}

func boundedDuration(name string, fallback time.Duration, minimum time.Duration, maximum time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < minimum || duration > maximum || duration%time.Second != 0 {
		return 0, errors.New(name + " is outside its allowed range")
	}
	return duration, nil
}

func boundedInt64(name string, fallback int64, minimum int64, maximum int64) (int64, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, errors.New(name + " is outside its allowed range")
	}
	return parsed, nil
}

func valueOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
