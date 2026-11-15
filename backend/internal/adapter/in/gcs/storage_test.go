package gcs

import (
	"context"
	"testing"
)

func TestNewStorage_ValidatesRequiredConfig(t *testing.T) {
	// given
	ctx := context.Background()

	// when
	_, missingBucketErr := NewStorage(ctx, Config{
		ServiceAccountEmail: "sem-api@example.iam.gserviceaccount.com",
	})
	_, missingSAErr := NewStorage(ctx, Config{
		Bucket: "sem-media",
	})

	// then
	if missingBucketErr == nil {
		t.Fatal("expected error when bucket is empty")
	}
	if missingSAErr == nil {
		t.Fatal("expected error when service account email is empty")
	}
}
