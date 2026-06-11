package secrets

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/99designs/keyring"

	"github.com/almahoozi/trace/internal/config"
)

const keyringServiceName = "t"
const tokenPaddingBytes = 16

type KeyringStore struct{}

func NewKeyringStore() Store {
	return KeyringStore{}
}

func (KeyringStore) LoadToken(cfg config.Config) (string, error) {
	if token, ok := resolveTokenFromEnv(cfg); ok {
		return token, nil
	}

	ring, err := openKeyring()
	if err != nil {
		return "", err
	}

	account, err := keyringAccount(cfg)
	if err != nil {
		return "", err
	}

	item, err := ring.Get(account)
	if err != nil {
		if errors.Is(err, keyring.ErrKeyNotFound) {
			return "", fmt.Errorf("set %s or save token to keyring account %q", tokenEnv(cfg), account)
		}
		return "", fmt.Errorf("read token from keyring account %q: %w", account, err)
	}
	if !storedTokenMatchesAccount(item, account) {
		return "", fmt.Errorf("set %s or save token to keyring account %q", tokenEnv(cfg), account)
	}

	tokenData, err := unpadToken(item.Data)
	if err != nil {
		return "", fmt.Errorf("read token from keyring account %q: %w", account, err)
	}

	token := string(tokenData)
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("empty token in keyring account %q", account)
	}

	return token, nil
}

func (KeyringStore) SaveToken(cfg config.Config, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token is empty")
	}

	ring, err := openKeyring()
	if err != nil {
		return err
	}

	account, err := keyringAccount(cfg)
	if err != nil {
		return err
	}

	paddedToken, err := padToken([]byte(token))
	if err != nil {
		return err
	}

	label := fmt.Sprintf("t://%s", accountLabelPart(account))

	if err := ring.Set(keyring.Item{
		Key:   account,
		Data:  paddedToken,
		Label: label,
	}); err != nil {
		return fmt.Errorf("save token to keyring account %q: %w", account, err)
	}

	return nil
}

func (KeyringStore) TokenLocation(cfg config.Config) (string, error) {
	account, err := keyringAccount(cfg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("system keyring (service %q, account %q)", keyringServiceName, account), nil
}

func openKeyring() (keyring.Keyring, error) {
	ring, err := keyring.Open(keyring.Config{
		ServiceName:              keyringServiceName,
		KeychainTrustApplication: true,
		AllowedBackends:          allowedBackends(),
	})
	if err != nil {
		return nil, fmt.Errorf("open keyring service %q: %w", keyringServiceName, err)
	}
	return ring, nil
}

func allowedBackends() []keyring.BackendType {
	switch runtime.GOOS {
	case "darwin":
		return []keyring.BackendType{
			keyring.KeychainBackend,
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.WinCredBackend,
			keyring.PassBackend,
		}
	case "windows":
		return []keyring.BackendType{
			keyring.WinCredBackend,
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.PassBackend,
			keyring.KeychainBackend,
		}
	default:
		return []keyring.BackendType{
			keyring.SecretServiceBackend,
			keyring.KWalletBackend,
			keyring.PassBackend,
			keyring.KeychainBackend,
			keyring.WinCredBackend,
		}
	}
}

func resolveTokenFromEnv(cfg config.Config) (string, bool) {
	envKey := tokenEnv(cfg)
	token, ok := resolveTokenFromSpecificEnv(envKey)
	if !ok {
		return "", false
	}
	return token, true
}

func tokenEnv(cfg config.Config) string {
	envKey := strings.TrimSpace(cfg.Auth.TokenEnv)
	if envKey == "" {
		return "TRACE_GRAFANA_TOKEN"
	}
	return envKey
}

func resolveTokenFromSpecificEnv(envKey string) (string, bool) {
	token, ok := os.LookupEnv(envKey)
	if !ok {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

func keyringAccount(cfg config.Config) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Grafana.BaseURL), "/")
	if baseURL == "" {
		return "", errors.New("grafana.base_url is empty")
	}
	return baseURL, nil
}

func accountLabelPart(account string) string {
	withoutScheme := strings.TrimPrefix(account, "https://")
	withoutScheme = strings.TrimPrefix(withoutScheme, "http://")
	return withoutScheme
}

func padToken(token []byte) ([]byte, error) {
	prefix := make([]byte, tokenPaddingBytes)
	if _, err := rand.Read(prefix); err != nil {
		return nil, fmt.Errorf("generate token prefix padding: %w", err)
	}
	suffix := make([]byte, tokenPaddingBytes)
	if _, err := rand.Read(suffix); err != nil {
		return nil, fmt.Errorf("generate token suffix padding: %w", err)
	}

	padded := make([]byte, 0, tokenPaddingBytes+len(token)+tokenPaddingBytes)
	padded = append(padded, prefix...)
	padded = append(padded, token...)
	padded = append(padded, suffix...)
	return padded, nil
}

func unpadToken(padded []byte) ([]byte, error) {
	if len(padded) < tokenPaddingBytes*2 {
		return nil, errors.New("stored token is too short")
	}
	return padded[tokenPaddingBytes : len(padded)-tokenPaddingBytes], nil
}

func storedTokenMatchesAccount(item keyring.Item, account string) bool {
	if item.Key != "" && item.Key != account {
		return false
	}
	expectedLabel := fmt.Sprintf("t://%s", accountLabelPart(account))
	if item.Label != "" && item.Label != expectedLabel {
		return false
	}
	return true
}
