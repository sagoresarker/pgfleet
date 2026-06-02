// Package objectstore manages the S3/MinIO backup repository bucket used by
// pgBackRest. The control plane ensures the bucket exists; pgBackRest writes
// per-instance path prefixes within it.
package objectstore

import (
	"context"
	"errors"
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
