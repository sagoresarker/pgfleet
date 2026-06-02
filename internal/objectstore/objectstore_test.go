package objectstore

import "testing"

func TestConfigValidate(t *testing.T) {
	valid := Config{Endpoint: "minio:9000", Region: "us-east-1", AccessKey: "k", SecretKey: "s", Bucket: "b"}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}

	for name, c := range map[string]Config{
		"no endpoint": {Region: "us-east-1", AccessKey: "k", SecretKey: "s", Bucket: "b"},
		"no bucket":   {Endpoint: "minio:9000", Region: "us-east-1", AccessKey: "k", SecretKey: "s"},
		"no key":      {Endpoint: "minio:9000", Region: "us-east-1", SecretKey: "s", Bucket: "b"},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestEndpointURLAddsScheme(t *testing.T) {
	if got := (Config{Endpoint: "minio:9000"}).endpointURL(); got != "http://minio:9000" {
		t.Errorf("endpointURL = %q, want http://minio:9000", got)
	}
	if got := (Config{Endpoint: "https://s3.amazonaws.com", UseTLS: true}).endpointURL(); got != "https://s3.amazonaws.com" {
		t.Errorf("endpointURL = %q, should keep explicit scheme", got)
	}
	if got := (Config{Endpoint: "s3.amazonaws.com", UseTLS: true}).endpointURL(); got != "https://s3.amazonaws.com" {
		t.Errorf("endpointURL = %q, want https when UseTLS", got)
	}
}
