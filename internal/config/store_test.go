package config_test

import (
	"testing"
	"time"
)

func TestValidateStoreRejectsUnsupportedDrivers(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = "unknown"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported meta store driver error")
	}

	cfg = loadDefaultConfig(t)
	cfg.Store.Object.Driver = "unknown"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected unsupported object store driver error")
	}
}

func TestValidateStoreAcceptsS3ObjectDriver(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "S3"
	cfg.Store.Object.Path = ""
	cfg.Store.Object.S3.Bucket = "regimux-objects"
	cfg.Store.Object.S3.Region = "us-east-1"
	cfg.Store.Object.S3.Prefix = "/cache/"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize s3 object store: %v", normalizeErr)
	}
	if cfg.Store.Object.Driver != "s3" || cfg.Store.Object.Path != "" || cfg.Store.Object.S3.Prefix != "cache" {
		t.Fatalf("unexpected object store config: %#v", cfg.Store.Object)
	}
}

func TestValidateStoreRejectsS3ObjectWithoutBucket(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "s3"
	cfg.Store.Object.S3.Bucket = ""
	cfg.Store.Object.S3.Region = "us-east-1"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected s3 bucket validation error")
	}
}

func TestValidateStoreRejectsPartialS3Credentials(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "s3"
	cfg.Store.Object.S3.Bucket = "regimux-objects"
	cfg.Store.Object.S3.Region = "us-east-1"
	cfg.Store.Object.S3.AccessKeyID = "access-key"
	cfg.Store.Object.S3.SecretAccessKey = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected s3 partial credentials validation error")
	}
}

func TestValidateStoreAcceptsSFTPObjectDriver(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "SFTP"
	cfg.Store.Object.Path = "/srv/regimux/objects"
	cfg.Store.Object.SFTP.Addr = "sftp.example.com:22"
	cfg.Store.Object.SFTP.Username = "regimux"
	cfg.Store.Object.SFTP.Password = "secret"
	cfg.Store.Object.SFTP.HostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeHostKeyForConfigValidationOnly"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize sftp object store: %v", normalizeErr)
	}
	if cfg.Store.Object.Driver != "sftp" || cfg.Store.Object.Path != "/srv/regimux/objects" || cfg.Store.Object.SFTP.Timeout != 10*time.Second {
		t.Fatalf("unexpected object store config: %#v", cfg.Store.Object)
	}
}

func TestValidateStoreRejectsSFTPObjectWithoutHostKeyVerification(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "sftp"
	cfg.Store.Object.SFTP.Addr = "sftp.example.com:22"
	cfg.Store.Object.SFTP.Username = "regimux"
	cfg.Store.Object.SFTP.Password = "secret"
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected sftp host key validation error")
	}
}

func TestValidateStoreAcceptsExternalMetaDrivers(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres", "pg", "postgresql"} {
		assertExternalMetaDriverAccepted(t, driver)
	}
}

func assertExternalMetaDriverAccepted(t *testing.T, driver string) {
	t.Helper()

	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = driver
	cfg.Store.Meta.DSN = "metadata-dsn"
	cfg.Store.Meta.Path = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize external meta store: %v", normalizeErr)
	}
	if cfg.Store.Meta.Driver != "mysql" && cfg.Store.Meta.Driver != "postgres" {
		t.Fatalf("unexpected normalized meta driver: %#v", cfg.Store.Meta)
	}
}

func TestValidateStoreRejectsExternalMetaWithoutDSN(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Meta.Driver = "postgres"
	cfg.Store.Meta.DSN = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr == nil {
		t.Fatal("expected external meta dsn validation error")
	}
}

func TestValidateStoreAcceptsMemoryObjectDriver(t *testing.T) {
	cfg := loadDefaultConfig(t)
	cfg.Store.Object.Driver = "MEMORY"
	cfg.Store.Object.Path = ""
	if normalizeErr := cfg.NormalizeAndValidate(); normalizeErr != nil {
		t.Fatalf("normalize memory object store: %v", normalizeErr)
	}
	if cfg.Store.Object.Driver != "memory" || cfg.Store.Object.Path != "data/objects" {
		t.Fatalf("unexpected object store config: %#v", cfg.Store.Object)
	}
}
