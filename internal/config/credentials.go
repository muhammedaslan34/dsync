package config

import (
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"
)

const credentialService = "dsync"

// ErrCredentialNotFound is returned when the operating system credential
// store has no secret for an account.
var ErrCredentialNotFound = keyring.ErrNotFound

// CredentialStore is the small part of an OS keychain that dsync needs.
// Production uses Keychain, Credential Manager, or Secret Service; tests can
// inject a deterministic in-memory implementation.
type CredentialStore interface {
	Set(service, account, secret string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

type systemCredentialStore struct{}

func (systemCredentialStore) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (systemCredentialStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}
func (systemCredentialStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

func (c *Config) sunshineCredentialAccount() string { return "sunshine:" + c.ID }

// loadSunshineCredential migrates the old plaintext field, or loads the
// current password from the OS credential store. A migration failure stops
// startup and leaves config.json untouched, so the only remaining copy is
// not silently destroyed. Normal keychain read failures are non-fatal: remote
// control stays disabled until the user saves the login again or removes it.
func (c *Config) loadSunshineCredential(legacyPassword string) (bool, error) {
	if legacyPassword != "" {
		if c.SunshineUser == "" {
			return true, nil // unusable orphan; Save removes it from config.json
		}
		if err := c.credentials.Set(credentialService, c.sunshineCredentialAccount(), legacyPassword); err != nil {
			return false, fmt.Errorf("move Sunshine password to the OS credential store: %w (config.json was left unchanged)", err)
		}
		c.SunshinePassword = legacyPassword
		return true, nil
	}
	if c.SunshineUser == "" {
		return false, nil
	}
	password, err := c.credentials.Get(credentialService, c.sunshineCredentialAccount())
	if err != nil {
		if errors.Is(err, ErrCredentialNotFound) {
			c.sunshineCredentialErr = errors.New("saved Sunshine password was not found in the OS credential store; enter it again or remove the saved login")
		} else {
			c.sunshineCredentialErr = fmt.Errorf("read Sunshine password from the OS credential store: %w", err)
		}
		return false, nil
	}
	c.SunshinePassword = password
	return false, nil
}

// SunshineCredentialError reports why a saved password could not be loaded.
// It is intentionally non-fatal so text and file sharing still work on a
// headless Linux session without Secret Service.
func (c *Config) SunshineCredentialError() error { return c.sunshineCredentialErr }

// SetSunshineCredentials stores or removes the Sunshine login transactionally.
// The password is never written to config.json and there is no plaintext
// fallback when the OS credential store is unavailable.
func (c *Config) SetSunshineCredentials(user, password string) error {
	oldUser, oldPassword, oldCredentialErr := c.SunshineUser, c.SunshinePassword, c.sunshineCredentialErr
	account := c.sunshineCredentialAccount()
	if user == "" {
		if err := c.credentials.Delete(credentialService, account); err != nil && !errors.Is(err, ErrCredentialNotFound) {
			return fmt.Errorf("remove Sunshine password from the OS credential store: %w", err)
		}
		c.SunshineUser, c.SunshinePassword, c.sunshineCredentialErr = "", "", nil
		if saveErr := c.Save(); saveErr != nil {
			var rollbackErr error
			if oldPassword != "" {
				rollbackErr = c.credentials.Set(credentialService, account, oldPassword)
			}
			if rollbackErr != nil {
				joined := errors.Join(saveErr, fmt.Errorf("restore Sunshine password after config save failure: %w", rollbackErr))
				// Deletion succeeded and restoration failed: keep runtime state
				// aligned with the now-empty credential store.
				c.sunshineCredentialErr = joined
				return joined
			}
			c.SunshineUser, c.SunshinePassword, c.sunshineCredentialErr = oldUser, oldPassword, oldCredentialErr
			return saveErr
		}
		return nil
	}

	if err := c.credentials.Set(credentialService, account, password); err != nil {
		return fmt.Errorf("save Sunshine password in the OS credential store: %w", err)
	}
	c.SunshineUser, c.SunshinePassword, c.sunshineCredentialErr = user, password, nil
	if saveErr := c.Save(); saveErr != nil {
		var rollbackErr error
		if oldPassword != "" {
			rollbackErr = c.credentials.Set(credentialService, account, oldPassword)
		} else {
			rollbackErr = c.credentials.Delete(credentialService, account)
			if errors.Is(rollbackErr, ErrCredentialNotFound) {
				rollbackErr = nil
			}
		}
		if rollbackErr != nil {
			joined := errors.Join(saveErr, fmt.Errorf("restore OS credential store after config save failure: %w", rollbackErr))
			// The new keychain value still exists. Keep runtime state aligned
			// with it instead of pretending the old value was restored.
			c.sunshineCredentialErr = joined
			return joined
		}
		c.SunshineUser, c.SunshinePassword, c.sunshineCredentialErr = oldUser, oldPassword, oldCredentialErr
		return saveErr
	}
	return nil
}
