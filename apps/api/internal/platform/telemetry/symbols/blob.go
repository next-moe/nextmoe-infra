package symbols

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"api/pkg/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type BlobStore interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

func BlobKey(sha256 string) string {
	return "blobs/" + sha256 + ".gz"
}

type gzipReadCloser struct {
	gz *gzip.Reader
	rc io.ReadCloser
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }

func (g *gzipReadCloser) Close() error {
	err1 := g.gz.Close()
	err2 := g.rc.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func wrapGzip(rc io.ReadCloser) (io.ReadCloser, error) {
	gz, err := gzip.NewReader(rc)
	if err != nil {
		_ = rc.Close()
		return nil, err
	}
	return &gzipReadCloser{gz: gz, rc: rc}, nil
}

type FSStore struct {
	root string
}

func NewFSStore(root string) *FSStore {
	return &FSStore{root: root}
}

func (s *FSStore) abs(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid blob key")
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func (s *FSStore) Put(_ context.Context, key string, r io.Reader) error {
	dest, err := s.abs(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".blob-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	gz := gzip.NewWriter(tmp)
	_, copyErr := io.Copy(gz, r)
	closeErr := gz.Close()
	syncErr := tmp.Sync()
	fileErr := tmp.Close()
	if copyErr != nil || closeErr != nil || syncErr != nil || fileErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if syncErr != nil {
			return syncErr
		}
		return fileErr
	}
	return os.Rename(tmpName, dest)
}

func (s *FSStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	dest, err := s.abs(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(dest)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		return nil, err
	}
	return wrapGzip(f)
}

func (s *FSStore) Delete(_ context.Context, key string) error {
	dest, err := s.abs(key)
	if err != nil {
		return err
	}
	err = os.Remove(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

type S3Store struct {
	s3     *s3.Client
	bucket string
}

func NewS3Store(cfg config.S3Config) (*S3Store, error) {
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, errors.New("telemetry s3: AccessKeyID and SecretAccessKey are required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("telemetry s3: Bucket is required")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry s3: load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	return &S3Store{s3: client, bucket: cfg.Bucket}, nil
}

func (s *S3Store) Put(ctx context.Context, key string, r io.Reader) error {
	tmp, err := os.CreateTemp("", "tel-s3-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	gz := gzip.NewWriter(tmp)
	_, copyErr := io.Copy(gz, r)
	closeErr := gz.Close()
	if copyErr != nil || closeErr != nil {
		_ = tmp.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		return err
	}
	st, err := tmp.Stat()
	if err != nil {
		_ = tmp.Close()
		return err
	}
	_, err = s.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          tmp,
		ContentLength: aws.Int64(st.Size()),
	})
	closeFile := tmp.Close()
	if err != nil {
		return fmt.Errorf("telemetry s3 put %q: %w", key, err)
	}
	return closeFile
}

func (s *S3Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if s3Missing(err) {
			return nil, fmt.Errorf("telemetry s3 get %q: %w", key, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("telemetry s3 get %q: %w", key, err)
	}
	return wrapGzip(out.Body)
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("telemetry s3 delete %q: %w", key, err)
	}
	return nil
}

func s3Missing(err error) bool {
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var respErr *smithyhttp.ResponseError
	return errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404
}
