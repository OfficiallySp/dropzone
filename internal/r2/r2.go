// Package r2 wraps the AWS SDK for Go v2 to talk to a Cloudflare R2 bucket
// over the S3-compatible API.
package r2

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is a thin R2 uploader.
type Client struct {
	s3            *s3.Client
	uploader      *manager.Uploader
	bucket        string
	publicBaseURL string
}

// New builds an R2 client. Region is fixed to "auto" and the endpoint is the
// account-level R2 host; the SDK injects the bucket as a virtual-host subdomain.
func New(ctx context.Context, accountID, accessKeyID, secret, bucket, publicBaseURL string) (*Client, error) {
	if accountID == "" || accessKeyID == "" || secret == "" || bucket == "" {
		return nil, fmt.Errorf("r2: missing credentials or bucket")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secret, ""),
		),
	)
	if err != nil {
		return nil, err
	}
	s3c := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID))
	})
	up := manager.NewUploader(s3c, func(u *manager.Uploader) {
		u.PartSize = 16 * 1024 * 1024 // 16 MiB parts keep large videos well under the 10k-part cap
		u.Concurrency = 4
	})
	return &Client{s3: s3c, uploader: up, bucket: bucket, publicBaseURL: publicBaseURL}, nil
}

// PutBytes uploads in-memory data (used for compressed images).
func (c *Client) PutBytes(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

// PutFile streams a file from disk (used for original + compressed videos).
// *os.File is an io.ReadSeeker, so the multipart uploader reads ranges directly
// without buffering the whole file in memory.
func (c *Client) PutFile(ctx context.Context, key, path, contentType string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = c.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        f,
		ContentType: aws.String(contentType),
	})
	return err
}

// Delete removes an object (used to drop the original after a compressed
// replacement lands under a different key/extension).
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// Verify checks that the credentials and bucket are usable.
func (c *Client) Verify(ctx context.Context) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	return err
}

// PublicURL builds the shareable URL for a key using the configured custom domain.
func (c *Client) PublicURL(key string) string {
	if c.publicBaseURL == "" {
		return key
	}
	return strings.TrimRight(c.publicBaseURL, "/") + "/" + strings.TrimLeft(key, "/")
}
