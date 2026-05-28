package config

import (
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/samber/oops"
)

func newConfigValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterStructValidation(validateRegistryAuthStruct, RegistryAuthConfig{})
	v.RegisterStructValidation(validateCacheStruct, CacheConfig{})
	v.RegisterStructValidation(validateStoreMetaStruct, StoreMetaConfig{})
	v.RegisterStructValidation(validateStoreObjectStruct, StoreObjectConfig{})
	return v
}

func validateConfig(cfg *Config) error {
	if cfg == nil {
		return oops.In("config").Errorf("config is nil")
	}
	if err := newConfigValidator().Struct(cfg); err != nil {
		return oops.In("config").Wrapf(err, "validate config")
	}
	return nil
}

func validateRegistryAuthStruct(sl validator.StructLevel) {
	auth, ok := sl.Current().Interface().(RegistryAuthConfig)
	if !ok || !auth.Enabled {
		return
	}
	if strings.TrimSpace(auth.TokenSecret) == "" {
		sl.ReportError(auth.TokenSecret, "token_secret", "TokenSecret", "required_with_auth", "")
	}
	if len(auth.Users) == 0 {
		sl.ReportError(auth.Users, "users", "Users", "required_with_auth", "")
		return
	}
	for username, user := range auth.Users {
		validateRegistryAuthUser(sl, username, user)
	}
}

func validateRegistryAuthUser(sl validator.StructLevel, username string, user AuthUserConfig) {
	if strings.TrimSpace(username) == "" {
		sl.ReportError(username, "users", "Users", "required_key", "")
	}
	if strings.TrimSpace(user.Password) == "" && strings.TrimSpace(user.PasswordHash) == "" {
		sl.ReportError(user.Password, "password", "Password", "password_or_hash", "")
	}
	for _, repo := range user.Repositories {
		if strings.TrimSpace(repo) == "" {
			sl.ReportError(repo, "repositories", "Repositories", "required", "")
		}
	}
}

func validateCacheStruct(sl validator.StructLevel) {
	cache, ok := sl.Current().Interface().(CacheConfig)
	if !ok {
		return
	}
	switch cache.Backend {
	case "redis":
		if len(cache.Redis.Addrs) == 0 {
			sl.ReportError(cache.Redis.Addrs, "redis.addrs", "Redis.Addrs", "required_with_redis", "")
		}
	case "valkey":
		if len(cache.Valkey.Addrs) == 0 {
			sl.ReportError(cache.Valkey.Addrs, "valkey.addrs", "Valkey.Addrs", "required_with_valkey", "")
		}
	}
}

func validateStoreMetaStruct(sl validator.StructLevel) {
	meta, ok := sl.Current().Interface().(StoreMetaConfig)
	if !ok {
		return
	}
	switch meta.Driver {
	case "mysql", "postgres":
		if strings.TrimSpace(meta.DSN) == "" {
			sl.ReportError(meta.DSN, "dsn", "DSN", "required_with_external_meta", meta.Driver)
		}
	}
}

func validateStoreObjectStruct(sl validator.StructLevel) {
	object, ok := sl.Current().Interface().(StoreObjectConfig)
	if !ok {
		return
	}
	validateStoreObjectS3(sl, object)
	validateStoreObjectSFTP(sl, object)
}

func validateStoreObjectS3(sl validator.StructLevel, object StoreObjectConfig) {
	if object.Driver != "s3" {
		return
	}
	reportBlankConfigValue(sl, object.S3.Bucket, "s3.bucket", "S3.Bucket", "required_with_s3_object_store")
	reportBlankConfigValue(sl, object.S3.Region, "s3.region", "S3.Region", "required_with_s3_object_store")
	if strings.TrimSpace(object.S3.AccessKeyID) == "" && strings.TrimSpace(object.S3.SecretAccessKey) != "" {
		sl.ReportError(object.S3.AccessKeyID, "s3.access_key_id", "S3.AccessKeyID", "required_with_secret_access_key", "")
	}
	if strings.TrimSpace(object.S3.SecretAccessKey) == "" && strings.TrimSpace(object.S3.AccessKeyID) != "" {
		sl.ReportError(object.S3.SecretAccessKey, "s3.secret_access_key", "S3.SecretAccessKey", "required_with_access_key_id", "")
	}
}

func validateStoreObjectSFTP(sl validator.StructLevel, object StoreObjectConfig) {
	if object.Driver != "sftp" {
		return
	}
	reportBlankConfigValue(sl, object.SFTP.Addr, "sftp.addr", "SFTP.Addr", "required_with_sftp_object_store")
	reportBlankConfigValue(sl, object.SFTP.Username, "sftp.username", "SFTP.Username", "required_with_sftp_object_store")
	if strings.TrimSpace(object.SFTP.Password) == "" && strings.TrimSpace(object.SFTP.PrivateKey) == "" {
		sl.ReportError(object.SFTP.Password, "sftp.password", "SFTP.Password", "password_or_private_key", "")
	}
	if strings.TrimSpace(object.SFTP.KnownHostsPath) == "" && strings.TrimSpace(object.SFTP.HostKey) == "" {
		sl.ReportError(object.SFTP.KnownHostsPath, "sftp.known_hosts_path", "SFTP.KnownHostsPath", "known_hosts_or_host_key", "")
	}
}

func reportBlankConfigValue(sl validator.StructLevel, value, field, structField, tag string) {
	if strings.TrimSpace(value) == "" {
		sl.ReportError(value, field, structField, tag, "")
	}
}
