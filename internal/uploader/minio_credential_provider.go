package uploader

import (
	"os"

	"github.com/minio/minio-go/pkg/credentials"
)

// A EnvAWS retrieves credentials from the environment variables of the
// running process. EnvAWSironment credentials never expire.
//
// EnvGeneric variables used:
//
// * Access Key ID:     S3_ACCESS_KEY
// * Secret Access Key: S3_SECRET_KEY
type EnvGeneric struct {
	retrieved bool
}

// Retrieve retrieves the keys from the environment.
func (e *EnvGeneric) Retrieve() (credentials.Value, error) {
	e.retrieved = false

	id := os.Getenv("S3_ACCESS_KEY")
	secret := os.Getenv("S3_SECRET_KEY")

	signerType := credentials.SignatureV4
	if id == "" || secret == "" {
		signerType = credentials.SignatureAnonymous
	}

	e.retrieved = true
	return credentials.Value{
		AccessKeyID:     id,
		SecretAccessKey: secret,
		SignerType:      signerType,
	}, nil
}

// IsExpired returns if the credentials have been retrieved.
func (e *EnvGeneric) IsExpired() bool {
	return !e.retrieved
}
