package secrets

import "github.com/almahoozi/trace/internal/config"

// Store abstracts credential retrieval/persistence.
// Current implementation intentionally preserves existing insecure behavior
// (env var + local token file) and is a seam for future secure backends.
type Store interface {
	LoadToken(cfg config.Config) (string, error)
	SaveToken(cfg config.Config, token string) error
	TokenLocation(cfg config.Config) (string, error)
}

type InsecureFileEnvStore struct{}

func NewStore(config.Config) Store {
	return InsecureFileEnvStore{}
}

func (InsecureFileEnvStore) LoadToken(cfg config.Config) (string, error) {
	return config.ResolveToken(cfg)
}

func (InsecureFileEnvStore) SaveToken(cfg config.Config, token string) error {
	return config.SaveToken(cfg, token)
}

func (InsecureFileEnvStore) TokenLocation(cfg config.Config) (string, error) {
	return config.TokenFilePath(cfg)
}
