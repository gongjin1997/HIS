package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/kinstall/kinstall/internal/domain"
)

// newAWSS3Clients builds an aws-sdk-go-v2 S3 client tuned for Huawei Cloud OBS, Aliyun OSS,
// Tencent COS, SeaweedFS, and Ceph RGW (D3 §7.3, T-M1-12):
//   - BaseEndpoint + UsePathStyle
//   - RequestChecksumCalculationWhenRequired + ResponseChecksumValidationWhenRequired (prevents aws-chunked incompatibility)
func newAWSS3Clients(cfg domain.MediaSourceConfig, ak, sk string, hc *http.Client) (*s3.Client, *s3.PresignClient) {
	region := cfg.Region
	if region == "" {
		region = "cn-north-4"
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	var credsProvider aws.CredentialsProvider
	if ak != "" || sk != "" {
		credsProvider = credentials.NewStaticCredentialsProvider(ak, sk, "")
	} else {
		credsProvider = aws.AnonymousCredentials{}
	}
	opts := s3.Options{
		Region:                     region,
		Credentials:                credsProvider,
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	if endpoint != "" {
		opts.BaseEndpoint = aws.String(endpoint)
	}
	if hc != nil {
		opts.HTTPClient = hc
	}
	cli := s3.New(opts)
	return cli, s3.NewPresignClient(cli)
}

// PutObject uploads a local artifact stream to OBS / S3 without aws-chunked trailing checksums (`sync-to-obs`, T-M1-12/13).
func (s *ObjectStoreSource) PutObject(ctx context.Context, logicalPath string, body io.Reader, contentLength int64) error {
	if s.s3Client == nil {
		return fmt.Errorf("s3 client not initialized")
	}
	key := s.objectKey(logicalPath)
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if contentLength >= 0 {
		input.ContentLength = aws.Int64(contentLength)
	}
	_, err := s.s3Client.PutObject(ctx, input)
	return err
}

// PresignGetAWS produces a short-lived S3v4 presigned GET URL via aws-sdk-go-v2 PresignClient (TTL <= 30m).
func (s *ObjectStoreSource) PresignGetAWS(ctx context.Context, spec ArtifactSpec, ttl time.Duration) (string, string, error) {
	if s.presign == nil {
		return "", "", ErrPresignNotSupported
	}
	if ttl <= 0 || ttl > 30*time.Minute {
		ttl = 15 * time.Minute
	}
	key := s.objectKey(spec.LogicalPath)
	out, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", "", err
	}
	label := fmt.Sprintf("%s://%s/%s [PRESIGNED-REDACTED]", s.name, s.bucket, key)
	return out.URL, label, nil
}
