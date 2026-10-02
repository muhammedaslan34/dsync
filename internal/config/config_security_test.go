package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type fakeCredentialStore struct {
	secrets                   map[string]string
	getErr, setErr, deleteErr error
	setCalls, setErrAt        int
}

func newFakeCredentialStore() *fakeCredentialStore {
	return &fakeCredentialStore{secrets: map[string]string{}}
}
func credentialKey(service, account string) string { return service + "\x00" + account }
func (s *fakeCredentialStore) Set(service, account, secret string) error {
	s.setCalls++
	if s.setErr != nil && (s.setErrAt == 0 || s.setCalls == s.setErrAt) {
		return s.setErr
	}
	s.secrets[credentialKey(service, account)] = secret
	return nil
}

func blockConfigSave(t *testing.T, cfg *Config) {
	t.Helper()
	path := filepath.Join(cfg.Dir(), "config.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
func (s *fakeCredentialStore) Get(service, account string) (string, error) {
	if s.getErr != nil {
		return "", s.getErr
	}
	secret, ok := s.secrets[credentialKey(service, account)]
	if !ok {
		return "", ErrCredentialNotFound
	}
	return secret, nil
}
func (s *fakeCredentialStore) Delete(service, account string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	key := credentialKey(service, account)
	if _, ok := s.secrets[key]; !ok {
		return ErrCredentialNotFound
	}
	delete(s.secrets, key)
	return nil
}

func TestSaveAtomicallyReplacesPrivateConfig(t *testing.T) {
	dir := t.TempDir()
	c := &Config{ID: "id", Name: "first", Port: 47101, dir: dir}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c.Name = "second"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name": "second"`) {
		t.Fatalf("saved config: %s", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("config mode = %o, want 600", got)
		}
	}
	if temps, _ := filepath.Glob(filepath.Join(dir, ".config-*.tmp")); len(temps) != 0 {
		t.Fatalf("temporary files left behind: %v", temps)
	}
}

func TestSaveCleansTemporaryFileAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory cannot be replaced by a file on any supported OS.
	target := filepath.Join(dir, "config.json")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Config{ID: "id", Name: "name", Port: 47101, dir: dir}
	if err := c.Save(); err == nil {
		t.Fatal("Save succeeded despite an unreplaceable destination")
	}
	if temps, _ := filepath.Glob(filepath.Join(dir, ".config-*.tmp")); len(temps) != 0 {
		t.Fatalf("temporary files left behind: %v", temps)
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatalf("destination was damaged: %v", err)
	}
}

func TestLegacySunshinePasswordMigratesToCredentialStore(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"id":"device-id","name":"PC","port":47101,"sunshine_user":"admin","sunshine_password":"plain-secret"}`
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SunshinePassword != "plain-secret" {
		t.Fatal("migrated password was not loaded for this process")
	}
	if got := store.secrets[credentialKey(credentialService, "sunshine:device-id")]; got != "plain-secret" {
		t.Fatalf("credential store secret = %q", got)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "plain-secret") || strings.Contains(string(data), "sunshine_password") {
		t.Fatalf("plaintext credential remains in config: %s", data)
	}

	reloaded, err := LoadFromWithCredentialStore(dir, store)
	if err != nil || reloaded.SunshinePassword != "plain-secret" {
		t.Fatalf("reload: password=%q err=%v", reloaded.SunshinePassword, err)
	}
}

func TestLegacyCredentialMigrationFailureLeavesConfigUntouched(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"id":"device-id","name":"PC","port":47101,"sunshine_user":"admin","sunshine_password":"only-copy"}`
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newFakeCredentialStore()
	store.setErr = errors.New("keychain unavailable")
	if _, err := LoadFromWithCredentialStore(dir, store); err == nil || !strings.Contains(err.Error(), "left unchanged") {
		t.Fatalf("migration error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != legacy {
		t.Fatalf("failed migration rewrote config: %s", data)
	}
}

func TestSunshineCredentialsNeverFallBackToConfig(t *testing.T) {
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetSunshineCredentials("admin", "new-secret"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.Dir(), "config.json"))
	if strings.Contains(string(data), "new-secret") || strings.Contains(string(data), "sunshine_password") {
		t.Fatalf("password serialized to config: %s", data)
	}
	if err := cfg.SetSunshineCredentials("", ""); err != nil {
		t.Fatal(err)
	}
	if cfg.SunshineUser != "" || cfg.SunshinePassword != "" || len(store.secrets) != 0 {
		t.Fatalf("credential removal incomplete: user=%q password=%q store=%v", cfg.SunshineUser, cfg.SunshinePassword, store.secrets)
	}
}

func TestSunshineCredentialRemovalFailureKeepsSavedLogin(t *testing.T) {
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetSunshineCredentials("admin", "secret"); err != nil {
		t.Fatal(err)
	}
	store.deleteErr = errors.New("keychain locked")
	if err := cfg.SetSunshineCredentials("", ""); err == nil {
		t.Fatal("credential removal failure was ignored")
	}
	if cfg.SunshineUser != "admin" || cfg.SunshinePassword != "secret" {
		t.Fatalf("failed removal changed login: %q %q", cfg.SunshineUser, cfg.SunshinePassword)
	}
	if got := store.secrets[credentialKey(credentialService, "sunshine:"+cfg.ID)]; got != "secret" {
		t.Fatalf("failed removal changed keychain secret to %q", got)
	}
}

func TestCredentialSetRollbackFailureKeepsActualKeychainState(t *testing.T) {
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	blockConfigSave(t, cfg)
	rollbackErr := errors.New("keychain delete failed")
	store.deleteErr = rollbackErr
	err = cfg.SetSunshineCredentials("admin", "new-secret")
	if err == nil || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "config.json") {
		t.Fatalf("compound error = %v", err)
	}
	if cfg.SunshineUser != "admin" || cfg.SunshinePassword != "new-secret" {
		t.Fatalf("runtime state pretends rollback succeeded: %q %q", cfg.SunshineUser, cfg.SunshinePassword)
	}
	if cfg.SunshineCredentialError() == nil {
		t.Fatal("rollback failure was not retained for UI/status reporting")
	}
	if got := store.secrets[credentialKey(credentialService, "sunshine:"+cfg.ID)]; got != "new-secret" {
		t.Fatalf("keychain secret = %q, want actual new value", got)
	}
}

func TestCredentialRemovalRollbackFailureKeepsActualEmptyState(t *testing.T) {
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetSunshineCredentials("admin", "old-secret"); err != nil {
		t.Fatal(err)
	}
	blockConfigSave(t, cfg)
	rollbackErr := errors.New("keychain restore failed")
	store.setErr, store.setErrAt = rollbackErr, store.setCalls+1
	err = cfg.SetSunshineCredentials("", "")
	if err == nil || !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "config.json") {
		t.Fatalf("compound error = %v", err)
	}
	if cfg.SunshineUser != "" || cfg.SunshinePassword != "" {
		t.Fatalf("runtime state pretends removal rollback succeeded: %q %q", cfg.SunshineUser, cfg.SunshinePassword)
	}
	if cfg.SunshineCredentialError() == nil {
		t.Fatal("rollback failure was not retained for UI/status reporting")
	}
	if len(store.secrets) != 0 {
		t.Fatalf("keychain is not actually empty: %v", store.secrets)
	}
}

func TestCredentialStoreFailureDoesNotSavePlaintext(t *testing.T) {
	store := newFakeCredentialStore()
	cfg, err := LoadFromWithCredentialStore(t.TempDir(), store)
	if err != nil {
		t.Fatal(err)
	}
	store.setErr = errors.New("no Secret Service")
	if err := cfg.SetSunshineCredentials("admin", "do-not-save"); err == nil {
		t.Fatal("credential store failure was ignored")
	}
	if cfg.SunshineUser != "" || cfg.SunshinePassword != "" {
		t.Fatal("failed credential save changed runtime config")
	}
	data, _ := os.ReadFile(filepath.Join(cfg.Dir(), "config.json"))
	if strings.Contains(string(data), "do-not-save") {
		t.Fatalf("plaintext fallback used: %s", data)
	}
}

func TestUnavailableCredentialStoreIsNonFatalAfterMigration(t *testing.T) {
	dir := t.TempDir()
	data := `{"id":"device-id","name":"PC","port":47101,"sunshine_user":"admin"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newFakeCredentialStore()
	store.getErr = errors.New("no session bus")
	cfg, err := LoadFromWithCredentialStore(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SunshinePassword != "" || cfg.SunshineCredentialError() == nil {
		t.Fatalf("password=%q credential error=%v", cfg.SunshinePassword, cfg.SunshineCredentialError())
	}
}
