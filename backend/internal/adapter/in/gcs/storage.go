package gcs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	iamcredentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"cloud.google.com/go/storage"
	"github.com/kaanunsel/social-event-mapper/backend/internal/application/imageupload"
)

// Config contains the bucket and service account settings for GCS signed URLs.
type Config struct {
	Bucket              string
	ServiceAccountEmail string
}

// Storage is the GCS-backed implementation of imageupload.Storage.
type Storage struct {
	client              *storage.Client
	iamClient           *iamcredentials.IamCredentialsClient
	bucket              string
	serviceAccountEmail string
}

// NewStorage constructs a GCS Storage adapter using Application Default Credentials
// and IAM Credentials SignBlob so Workload Identity can sign V4 upload URLs without static keys.
func NewStorage(ctx context.Context, cfg Config) (*Storage, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, fmt.Errorf("gcs bucket is required")
	}
	saEmail := strings.TrimSpace(cfg.ServiceAccountEmail)
	if saEmail == "" {
		return nil, fmt.Errorf("gcs service account email is required")
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create gcs client: %w", err)
	}

	iamClient, err := iamcredentials.NewIamCredentialsClient(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("create iam credentials client: %w", err)
	}

	return &Storage{
		client:              client,
		iamClient:           iamClient,
		bucket:              bucket,
		serviceAccountEmail: saEmail,
	}, nil
}

// Close releases the underlying GCS and IAM Credentials clients.
func (s *Storage) Close() error {
	var errs []error
	if s.client != nil {
		errs = append(errs, s.client.Close())
	}
	if s.iamClient != nil {
		errs = append(errs, s.iamClient.Close())
	}
	return errors.Join(errs...)
}

// PresignPutObject returns a V4 signed PUT request for a single object key in GCS.
func (s *Storage) PresignPutObject(
	ctx context.Context,
	key, contentType, cacheControl string,
	expires time.Duration,
) (*imageupload.PresignedRequest, error) {
	url, err := storage.SignedURL(s.bucket, key, &storage.SignedURLOptions{
		Scheme:         storage.SigningSchemeV4,
		Method:         "PUT",
		ContentType:    contentType,
		Headers:        []string{"Cache-Control:" + cacheControl},
		Expires:        time.Now().Add(expires),
		GoogleAccessID: s.serviceAccountEmail,
		SignBytes: func(b []byte) ([]byte, error) {
			resp, signErr := s.iamClient.SignBlob(ctx, &credentialspb.SignBlobRequest{
				Name:    "projects/-/serviceAccounts/" + s.serviceAccountEmail,
				Payload: b,
			})
			if signErr != nil {
				return nil, fmt.Errorf("iam sign blob: %w", signErr)
			}
			return resp.SignedBlob, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("presign gcs put object %q: %w", key, err)
	}

	return &imageupload.PresignedRequest{
		Method: "PUT",
		URL:    url,
		Headers: map[string]string{
			"Content-Type":  contentType,
			"Cache-Control": cacheControl,
		},
	}, nil
}

// ObjectExists checks whether the requested object key is present in GCS.
func (s *Storage) ObjectExists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.Bucket(s.bucket).Object(key).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("stat gcs object %q: %w", key, err)
}
