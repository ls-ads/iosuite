package iocore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Client handles interactions with RunPod's S3-compatible API.
type S3Client struct {
	client *s3.Client
	bucket string
}

// NewS3Client creates a new S3 client configured for a specific RunPod region and volume.
func NewS3Client(ctx context.Context, region, accessKey, secretKey, volumeID string) (*S3Client, error) {
	endpoint := GetS3Endpoint(region)

	// RunPod requires path-style addressing and S3v4 signatures.
	// The region in the signature must match the endpoint region.
	regionID := strings.ToLower(strings.ReplaceAll(region, "_", "-"))

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(regionID),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load SDK config: %v", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.Region = regionID
		o.UsePathStyle = true
	})

	return &S3Client{
		client: client,
		bucket: volumeID,
	}, nil
}

// UploadFile uploads a local file to the S3 bucket.
func (s *S3Client) UploadFile(ctx context.Context, localPath, s3Key string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s3Key),
		Body:   file,
	})
	return err
}

// DownloadFile downloads a file from S3 to a local path.
func (s *S3Client) DownloadFile(ctx context.Context, s3Key, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s3Key),
	})
	if err != nil {
		return err
	}
	defer result.Body.Close()

	file, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, result.Body)
	return err
}

// DownloadDirectory downloads all files with a given prefix from S3.
func (s *S3Client) DownloadDirectory(ctx context.Context, s3Prefix, localBaseDir string) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s3Prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			if strings.HasSuffix(key, "/") {
				continue // Skip directory-like keys
			}

			// Calculate local path relative to prefix
			rel, err := filepath.Rel(s3Prefix, key)
			if err != nil {
				rel = key // Fallback
			}
			dest := filepath.Join(localBaseDir, rel)

			if err := s.DownloadFile(ctx, key, dest); err != nil {
				return err
			}
		}
	}

	return nil
}

// ObjectExists checks if a specific object exists in the bucket.
func (s *S3Client) ObjectExists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check for 404
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "NotFound") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DeletePrefix deletes all objects with the given prefix.
func (s *S3Client) DeletePrefix(ctx context.Context, prefix string) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}

		var objects []types.ObjectIdentifier
		for _, obj := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: obj.Key})
		}

		if len(objects) > 0 {
			_, err = s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(s.bucket),
				Delete: &types.Delete{Objects: objects},
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}
