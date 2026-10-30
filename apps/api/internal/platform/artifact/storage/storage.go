package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"api/pkg/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type Client struct {
	s3      *s3.Client
	presign *s3.PresignClient
	bucket  string
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

type UploadedPart struct {
	PartNumber int32
	ETag       string
	Size       int64
}

func NewClient(cfg config.S3Config) (*Client, error) {
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("artifact storage: AccessKeyID and SecretAccessKey are required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("artifact storage: Bucket is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("artifact storage: load aws config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	return &Client{
		s3:      s3Client,
		presign: s3.NewPresignClient(s3Client),
		bucket:  cfg.Bucket,
	}, nil
}

func (c *Client) Bucket() string { return c.bucket }

func (c *Client) PresignPut(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("artifact storage: presign put %q: %w", key, err)
	}
	return req.URL, nil
}

func (c *Client) PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error) {
	in := &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if downloadName != "" {
		in.ResponseContentDisposition = aws.String(ContentDisposition(downloadName))
	}
	req, err := c.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("artifact storage: presign get %q: %w", key, err)
	}
	return req.URL, nil
}

func (c *Client) CreateMultipart(ctx context.Context, key, downloadName, contentType string) (string, error) {
	in := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}
	if downloadName != "" {
		in.ContentDisposition = aws.String(ContentDisposition(downloadName))
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	out, err := c.s3.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", fmt.Errorf("artifact storage: create multipart %q: %w", key, err)
	}
	if out.UploadId == nil {
		return "", fmt.Errorf("artifact storage: create multipart %q: nil upload id", key)
	}
	return *out.UploadId, nil
}

func (c *Client) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32, ttl time.Duration) (string, error) {
	req, err := c.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("artifact storage: presign part %d of %q: %w", partNumber, key, err)
	}
	return req.URL, nil
}

func (c *Client) ListParts(ctx context.Context, key, uploadID string) ([]UploadedPart, error) {
	var out []UploadedPart
	var marker *string
	for {
		resp, err := c.s3.ListParts(ctx, &s3.ListPartsInput{
			Bucket:           aws.String(c.bucket),
			Key:              aws.String(key),
			UploadId:         aws.String(uploadID),
			PartNumberMarker: marker,
		})
		if err != nil {
			return nil, fmt.Errorf("artifact storage: list parts %q: %w", key, err)
		}
		for _, p := range resp.Parts {
			up := UploadedPart{}
			if p.PartNumber != nil {
				up.PartNumber = *p.PartNumber
			}
			if p.ETag != nil {
				up.ETag = *p.ETag
			}
			if p.Size != nil {
				up.Size = *p.Size
			}
			out = append(out, up)
		}
		if resp.IsTruncated != nil && *resp.IsTruncated {
			marker = resp.NextPartNumberMarker
			continue
		}
		break
	}
	return out, nil
}

func (c *Client) CompleteMultipart(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	_, err := c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(c.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return fmt.Errorf("artifact storage: complete multipart %q: %w", key, err)
	}
	return nil
}

func (c *Client) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("artifact storage: abort multipart %q: %w", key, err)
	}
	return nil
}

func IsNotFound(err error) bool {
	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
		return true
	}
	var notFound *types.NotFound
	return errors.As(err, &notFound)
}

func IsNoSuchUpload(err error) bool {
	var noSuch *types.NoSuchUpload
	if errors.As(err, &noSuch) {
		return true
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload"
}

func (c *Client) HeadSize(ctx context.Context, key string) (int64, error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, fmt.Errorf("artifact storage: head %q: %w", key, err)
	}
	if out.ContentLength == nil {
		return 0, nil
	}
	return *out.ContentLength, nil
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("artifact storage: head %q: %w", key, err)
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("artifact storage: delete %q: %w", key, err)
	}
	return nil
}

func (c *Client) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("artifact storage: put %q: %w", key, err)
	}
	return nil
}

func (c *Client) SetContentDisposition(ctx context.Context, key, downloadName, contentType string) error {
	in := &s3.CopyObjectInput{
		Bucket:             aws.String(c.bucket),
		Key:                aws.String(key),
		CopySource:         aws.String(c.bucket + "/" + key),
		MetadataDirective:  types.MetadataDirectiveReplace,
		ContentDisposition: aws.String(ContentDisposition(downloadName)),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if _, err := c.s3.CopyObject(ctx, in); err != nil {
		return fmt.Errorf("artifact storage: set content-disposition %q: %w", key, err)
	}
	return nil
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	_, err := c.s3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	_, err = c.s3.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(c.bucket)})
	if err != nil {
		var alreadyOwned *types.BucketAlreadyOwnedByYou
		var alreadyExists *types.BucketAlreadyExists
		if errors.As(err, &alreadyOwned) || errors.As(err, &alreadyExists) {
			return nil
		}
		return fmt.Errorf("artifact storage: create bucket %q: %w", c.bucket, err)
	}
	return nil
}

func ContentDisposition(name string) string {
	return fmt.Sprintf("attachment; filename*=UTF-8''%s", percentEncode(name))
}

func percentEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b []byte
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
			ch == '-' || ch == '_' || ch == '.' || ch == '~' {
			b = append(b, ch)
			continue
		}
		b = append(b, '%', hex[ch>>4], hex[ch&0x0f])
	}
	return string(b)
}
