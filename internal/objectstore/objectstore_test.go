package objectstore

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

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

// apiErr is a minimal smithy.APIError, the shape MinIO/S3 return for a
// missing object when the SDK cannot deserialize a typed modeled error.
type apiErr struct {
	code string
}

func (e apiErr) Error() string                 { return e.code }
func (e apiErr) ErrorCode() string             { return e.code }
func (e apiErr) ErrorMessage() string          { return e.code }
func (e apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

// TestGetObjectErrorMapping verifies that the "no such key"/404 condition from
// the underlying store is mapped to KindNotFound, while other errors stay
// KindInternal (OS-1).
func TestGetObjectErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want apperr.Code
	}{
		{"typed NoSuchKey", &types.NoSuchKey{}, apperr.KindNotFound},
		{"typed NotFound", &types.NotFound{}, apperr.KindNotFound},
		{"smithy NoSuchKey code", apiErr{code: "NoSuchKey"}, apperr.KindNotFound},
		{"smithy NotFound code", apiErr{code: "NotFound"}, apperr.KindNotFound},
		{"smithy 404 code", apiErr{code: "404"}, apperr.KindNotFound},
		{"other smithy error", apiErr{code: "AccessDenied"}, apperr.KindInternal},
		{"plain error", errors.New("boom"), apperr.KindInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapGetError(tc.err)
			if apperr.Kind(got) != tc.want {
				t.Errorf("mapGetError(%v) kind = %v, want %v", tc.err, apperr.Kind(got), tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Errorf("mapGetError should wrap the original cause")
			}
		})
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
