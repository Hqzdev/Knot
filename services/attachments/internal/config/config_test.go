package config

import "testing"

func TestLoadDefaultsToBoundedProductionConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	configuration, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Address != ":8082" || configuration.MetadataMode != "memory" || configuration.Limits.MaxSize != defaultMaxSize || configuration.CleanupBatch != defaultCleanupBatch || !configuration.S3.PathStyle {
		t.Fatalf("unexpected defaults: %#v", configuration)
	}
}

func TestLoadRejectsUnsafeLimits(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("KNOT_ATTACHMENT_UPLOAD_TTL", "8d")
	if _, err := Load(); err == nil {
		t.Fatal("expected unsafe presign TTL rejection")
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("KNOT_JWT_SECRET", "test-secret")
	t.Setenv("KNOT_ATTACHMENTS_STORE", "memory")
	t.Setenv("KNOT_S3_ENDPOINT", "http://127.0.0.1:9000")
	t.Setenv("KNOT_S3_BUCKET", "attachments")
	t.Setenv("KNOT_S3_ACCESS_KEY_ID", "access-key")
	t.Setenv("KNOT_S3_SECRET_ACCESS_KEY", "secret-key")
}
