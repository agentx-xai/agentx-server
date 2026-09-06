package s3

import (
	"agentx/server/internal/entity"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxArtifactSize int64 = 51 << 20

type ArtifactStore struct {
	client *minio.Client
	bucket string
}

func NewArtifactStore(endpoint, accessKey, secretKey, bucket string, secure bool) (*ArtifactStore, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || bucket == "" {
		return nil, fmt.Errorf("S3 endpoint and bucket are required")
	}
	client, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: secure})
	if err != nil {
		return nil, err
	}
	return &ArtifactStore{client: client, bucket: bucket}, nil
}

func (s *ArtifactStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code != "" && response.Code != "NoSuchBucket" && response.Code != "NotFound" {
			return err
		}
		exists = false
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
}
func (s *ArtifactStore) Ready(ctx context.Context) error { return s.EnsureBucket(ctx) }

func (s *ArtifactStore) Put(ctx context.Context, name string, src io.Reader) (entity.Release, error) {
	if strings.TrimSpace(name) == "" {
		return entity.Release{}, fmt.Errorf("package name is required")
	}
	data, err := io.ReadAll(io.LimitReader(src, maxArtifactSize+1))
	if err != nil {
		return entity.Release{}, err
	}
	if int64(len(data)) > maxArtifactSize {
		return entity.Release{}, fmt.Errorf("artifact exceeds %d bytes", maxArtifactSize)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if _, err := s.client.PutObject(ctx, s.bucket, digest, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		return entity.Release{}, err
	}
	return entity.Release{Name: name, SHA256: digest, Size: int64(len(data))}, nil
}

func (s *ArtifactStore) Open(ctx context.Context, digest string) (io.ReadCloser, error) {
	if !validDigest(digest) {
		return nil, fmt.Errorf("invalid digest")
	}
	if _, err := s.client.StatObject(ctx, s.bucket, digest, minio.StatObjectOptions{}); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, digest, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, maxArtifactSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxArtifactSize || fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, fmt.Errorf("artifact digest mismatch")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *ArtifactStore) Delete(ctx context.Context, digest string) error {
	if !validDigest(digest) {
		return fmt.Errorf("invalid digest")
	}
	return s.client.RemoveObject(ctx, s.bucket, digest, minio.RemoveObjectOptions{})
}

func (s *ArtifactStore) Stat(ctx context.Context, digest string) (entity.Release, error) {
	if !validDigest(digest) {
		return entity.Release{}, fmt.Errorf("invalid digest")
	}
	info, err := s.client.StatObject(ctx, s.bucket, digest, minio.StatObjectOptions{})
	if err != nil {
		return entity.Release{}, err
	}
	return entity.Release{SHA256: digest, Size: info.Size}, nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}
