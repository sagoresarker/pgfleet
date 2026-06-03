// Package objectstore manages the S3/MinIO backup repository bucket used by
// pgBackRest. The control plane ensures the bucket exists; pgBackRest writes
// per-instance path prefixes within it.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

// Config holds S3/MinIO connection settings.
type Config struct {
	Endpoint  string // host:port or URL
	Region    string
	AccessKey string
	SecretKey string
	Bucket    string
	UseTLS    bool
}

// Validate checks required fields.
func (c Config) Validate() error {
	switch {
	case c.Endpoint == "":
		return apperr.New(apperr.KindInvalid, "objectstore: endpoint required")
	case c.Bucket == "":
		return apperr.New(apperr.KindInvalid, "objectstore: bucket required")
	case c.AccessKey == "" || c.SecretKey == "":
		return apperr.New(apperr.KindInvalid, "objectstore: credentials required")
	}
	return nil
}

func (c Config) endpointURL() string {
	if strings.Contains(c.Endpoint, "://") {
		return c.Endpoint
	}
	if c.UseTLS {
		return "https://" + c.Endpoint
	}
	return "http://" + c.Endpoint
}

// newClient builds a path-style S3 client (required for MinIO).
func (c Config) newClient(ctx context.Context) (*s3.Client, error) {
	awsConf, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(c.Region),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "objectstore: load aws config", err)
	}
	return s3.NewFromConfig(awsConf, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(c.endpointURL())
		o.UsePathStyle = true
	}), nil
}

// BucketExists reports whether the configured bucket exists and is reachable.
func BucketExists(ctx context.Context, c Config) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return false, err
	}
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.Bucket)}); err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return false, nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NotFound" || apiErr.ErrorCode() == "NoSuchBucket") {
			return false, nil
		}
		return false, apperr.Wrap(apperr.KindInternal, "objectstore: head bucket", err)
	}
	return true, nil
}

// EnsureBucket creates the configured bucket if it does not already exist. It
// is idempotent and safe to call on every boot.
func EnsureBucket(ctx context.Context, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return err
	}

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.Bucket)})
	if err == nil {
		return nil // already exists and reachable
	}

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.Bucket)})
	if err != nil {
		// A concurrent create or pre-existing ownership is fine.
		var owned *types.BucketAlreadyOwnedByYou
		var exists *types.BucketAlreadyExists
		if errors.As(err, &owned) || errors.As(err, &exists) {
			return nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "BucketAlreadyOwnedByYou" {
			return nil
		}
		return apperr.Wrap(apperr.KindInternal, "objectstore: create bucket", err)
	}
	return nil
}

// PutObject writes data to the configured bucket under key.
func PutObject(ctx context.Context, c Config, key string, data []byte) error {
	if err := c.Validate(); err != nil {
		return err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "objectstore: put object", err)
	}
	return nil
}

// GetObject reads the object stored under key from the configured bucket.
func GetObject(ctx context.Context, c Config, key string) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return nil, err
	}
	out, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapGetError(err)
	}
	defer func() { _ = out.Body.Close() }()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "objectstore: read object body", err)
	}
	return data, nil
}

// mapGetError classifies an error returned by GetObject. A missing key (the
// modeled *types.NoSuchKey, or a generic NoSuchKey/NotFound/404 reported by the
// underlying S3/MinIO client) maps to KindNotFound; everything else is
// KindInternal. The original error is always wrapped as the cause.
func mapGetError(err error) error {
	var noSuchKey *types.NoSuchKey
	var notFound *types.NotFound
	if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
		return apperr.Wrap(apperr.KindNotFound, "objectstore: object not found", err)
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return apperr.Wrap(apperr.KindNotFound, "objectstore: object not found", err)
		}
	}
	return apperr.Wrap(apperr.KindInternal, "objectstore: get object", err)
}

// ListObjects returns the keys in the configured bucket that start with prefix.
func ListObjects(ctx context.Context, c Config, prefix string) ([]string, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return nil, err
	}
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.Bucket),
		Prefix: aws.String(prefix),
	})
	var keys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, apperr.Wrap(apperr.KindInternal, "objectstore: list objects", err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}
	return keys, nil
}

// DeleteObject removes the object stored under key from the configured bucket.
func DeleteObject(ctx context.Context, c Config, key string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	client, err := c.newClient(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return apperr.Wrap(apperr.KindInternal, "objectstore: delete object", err)
	}
	return nil
}
