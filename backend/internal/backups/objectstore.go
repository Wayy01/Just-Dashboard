package backups

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3Client builds a client for AWS S3 or any S3-compatible service.
// Backblaze B2 exposes an S3 API, so the only difference between the two
// targets is the endpoint and the need for path-style addressing.
func s3Client(ctx context.Context, job *Job, secrets *TargetSecrets) (*s3.Client, error) {
	region := job.Target.Region
	if region == "" {
		region = "us-east-1"
	}
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if secrets != nil && secrets.AccessKeyID != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(secrets.AccessKeyID, secrets.SecretAccessKey, ""),
		))
	}
	// Without explicit credentials the SDK's default chain applies, which
	// picks up an instance role — the right behaviour on an EC2 host.
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if job.Target.Endpoint != "" {
			endpoint := job.Target.Endpoint
			if len(endpoint) < 8 || (endpoint[:7] != "http://" && endpoint[:8] != "https://") {
				endpoint = "https://" + endpoint
			}
			o.BaseEndpoint = &endpoint
			o.UsePathStyle = true
		}
	}), nil
}

func uploadObject(ctx context.Context, job *Job, secrets *TargetSecrets, key, localPath string) error {
	client, err := s3Client(ctx, job, secrets)
	if err != nil {
		return err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// The manager splits large archives into concurrent multipart uploads,
	// which is what makes a multi-gigabyte backup finish in reasonable time.
	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 16 * 1024 * 1024
		u.Concurrency = 3
	})
	ctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: &job.Target.Bucket,
		Key:    &key,
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("upload to %s/%s failed: %w", job.Target.Bucket, key, err)
	}
	return nil
}

func deleteObject(ctx context.Context, job *Job, secrets *TargetSecrets, key string) error {
	client, err := s3Client(ctx, job, secrets)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &job.Target.Bucket, Key: &key})
	return err
}

// downloadObject streams an artifact back for a restore.
func downloadObject(ctx context.Context, job *Job, secrets *TargetSecrets, key, destPath string) error {
	client, err := s3Client(ctx, job, secrets)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Hour)
	defer cancel()
	obj, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: &job.Target.Bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("download of %s/%s failed: %w", job.Target.Bucket, key, err)
	}
	defer obj.Body.Close()

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, obj.Body)
	return err
}

// TestTarget verifies a destination is reachable and writable before an
// operator trusts a schedule to it. A backup configuration that only fails at
// 3am is worse than one that fails while somebody is looking at it.
func TestTarget(ctx context.Context, job *Job, secrets *TargetSecrets) error {
	if job.TargetKind == TargetLocal {
		if err := os.MkdirAll(job.Target.Path, 0o700); err != nil {
			return err
		}
		probe, err := os.CreateTemp(job.Target.Path, ".vpsd-probe-*")
		if err != nil {
			return fmt.Errorf("%s is not writable: %w", job.Target.Path, err)
		}
		probe.Close()
		return os.Remove(probe.Name())
	}
	client, err := s3Client(ctx, job, secrets)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &job.Target.Bucket})
	if err != nil {
		return fmt.Errorf("cannot reach bucket %s: %w", job.Target.Bucket, err)
	}
	return nil
}
