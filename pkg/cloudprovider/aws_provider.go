package cloudprovider

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	oadpv1alpha1 "github.com/openshift/oadp-operator/api/v1alpha1"
	"golang.org/x/exp/rand"
	"time"
)

// AWSProvider is an AWS-specific implementation of CloudProvider
type AWSProvider struct {
	client *s3.Client
}

// NewAWSProvider inits the AWS client with configuration
func NewAWSProvider(region, accessKey, secretAccessKey string) (*AWSProvider, error) {
	creds := credentials.NewStaticCredentialsProvider(accessKey, secretAccessKey, "")
	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(creds),
	)

	if err != nil {
		return nil, err
	}

	return &AWSProvider{client: s3.NewFromConfig(awsCfg)}, nil
}

// UploadTest uploads an object to S3 and returns the upload duration in milliseconds.
func (p *AWSProvider) UploadTest(ctx context.Context, ust *oadpv1alpha1.UploadSpeedTest, fileSize int64, testTimeout time.Duration) (int64, error) {
	start := time.Now()

	// Prepare upload data
	data := make([]byte, fileSize)
	if _, err := rand.Read(data); err != nil {
		return 0, fmt.Errorf("failed to generate random data: %v", err)
	}

	// Create a context with timeout for the upload operation
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	_, err := p.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(ust.Spec.BackupLocation.Velero.ObjectStorage.Bucket),
		Key:    aws.String(fmt.Sprintf("upload-speed-test-%d", time.Now().Unix())),
		Body:   bytes.NewReader(data),
	})

	if err != nil {
		return 0, fmt.Errorf("upload to s3 storage failed: %v", err)
	}

	return time.Since(start).Milliseconds(), nil
}
