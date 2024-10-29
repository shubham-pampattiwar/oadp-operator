package cloudprovider

import (
	"context"
	oadpv1alpha1 "github.com/openshift/oadp-operator/api/v1alpha1"
	"time"
)

type CloudProvider interface {
	UploadTest(ctx context.Context, ust *oadpv1alpha1.UploadSpeedTest, fileSize int64, testTimeout time.Duration) (int64, error)
}
