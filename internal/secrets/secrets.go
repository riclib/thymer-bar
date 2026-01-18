// Package secrets provides secure storage for API tokens and credentials.
// Uses the system keychain (macOS Keychain, Linux secret-service).
package secrets

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const serviceName = "thymer-bar"

// Set stores a secret in the system keychain.
func Set(key, value string) error {
	if err := keyring.Set(serviceName, key, value); err != nil {
		return fmt.Errorf("failed to store secret %q: %w", key, err)
	}
	return nil
}

// Get retrieves a secret from the system keychain.
// Returns empty string and nil error if not found.
func Get(key string) (string, error) {
	value, err := keyring.Get(serviceName, key)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret %q: %w", key, err)
	}
	return value, nil
}

// Delete removes a secret from the system keychain.
func Delete(key string) error {
	if err := keyring.Delete(serviceName, key); err != nil && err != keyring.ErrNotFound {
		return fmt.Errorf("failed to delete secret %q: %w", key, err)
	}
	return nil
}

// Has checks if a secret exists (without retrieving it).
func Has(key string) bool {
	_, err := keyring.Get(serviceName, key)
	return err == nil
}

// Well-known secret keys (convention, not enforced)
const (
	GitHubToken   = "github-token"
	LinearToken   = "linear-token"
	ReadwiseToken = "readwise-token"

	// Google OAuth (stored separately for flexibility)
	GoogleCredentials = "google-credentials" // JSON from Cloud Console
	GoogleOAuthToken  = "google-oauth-token" // Refresh token after auth
)

// KnownKeys returns all well-known secret keys for listing/validation.
func KnownKeys() []string {
	return []string{
		GitHubToken,
		LinearToken,
		ReadwiseToken,
		GoogleCredentials,
		GoogleOAuthToken,
	}
}
