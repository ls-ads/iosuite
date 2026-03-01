package iocore

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// S3Client handles interactions with RunPod's S3-compatible API using the aws-cli.
type S3Client struct {
	accessKey string
	secretKey string
	region    string
	endpoint  string
	bucket    string
}

// NewS3Client creates a new S3 client wrapper configured for a specific RunPod region and volume.
func NewS3Client(ctx context.Context, region, accessKey, secretKey, volumeID string) (*S3Client, error) {
	// Verify aws-cli is installed
	if _, err := exec.LookPath("aws"); err != nil {
		return nil, fmt.Errorf("aws-cli is required but not found in PATH: %w", err)
	}

	endpoint := GetS3Endpoint(region)
	regionID := strings.ToLower(strings.ReplaceAll(region, "_", "-"))

	return &S3Client{
		accessKey: accessKey,
		secretKey: secretKey,
		region:    regionID,
		endpoint:  endpoint,
		bucket:    volumeID,
	}, nil
}

// runAWSCommand executes an aws cli command with the necessary environment variables and arguments.
func (s *S3Client) runAWSCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "aws", args...)

	// Set up environment with strict credentials
	env := os.Environ()
	env = append(env, fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", s.accessKey))
	env = append(env, fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", s.secretKey))
	cmd.Env = env

	// Capture output for debugging
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws cli error: %w (output: %s)", err, string(out))
	}
	return nil
}

// UploadFile uploads a local file to the S3 bucket using aws s3 cp.
func (s *S3Client) UploadFile(ctx context.Context, localPath, s3Key string) error {
	s3URI := fmt.Sprintf("s3://%s/%s", s.bucket, s3Key)
	return s.runAWSCommand(ctx,
		"s3", "cp", localPath, s3URI,
		"--endpoint-url", s.endpoint,
		"--region", s.region,
	)
}

// DownloadFile downloads a file from S3 to a local path using aws s3 cp.
func (s *S3Client) DownloadFile(ctx context.Context, s3Key, localPath string) error {
	s3URI := fmt.Sprintf("s3://%s/%s", s.bucket, s3Key)
	return s.runAWSCommand(ctx,
		"s3", "cp", s3URI, localPath,
		"--endpoint-url", s.endpoint,
		"--region", s.region,
	)
}

// DeleteFile deletes a file from S3 using aws s3 rm.
func (s *S3Client) DeleteFile(ctx context.Context, s3Key string) error {
	s3URI := fmt.Sprintf("s3://%s/%s", s.bucket, s3Key)
	return s.runAWSCommand(ctx,
		"s3", "rm", s3URI,
		"--endpoint-url", s.endpoint,
		"--region", s.region,
	)
}
