package secrets

import (
	"errors"

	"github.com/Neha611/commhub/internal/safe"
	"github.com/zalando/go-keyring"
)

const service = "commhub"

type keyringBackend struct{}

func (k *keyringBackend) Name() string { return "keyring" }

// Available probes for a working Secret Service without writing anything.
// A "not found" answer proves the service responded; a transport error means
// there is no keychain here.
func (k *keyringBackend) Available() bool {
	_, err := keyring.Get(service, "commhub:probe:availability")
	return err == nil || errors.Is(err, keyring.ErrNotFound)
}

func (k *keyringBackend) Get(key string) (safe.Secret, error) {
	v, err := keyring.Get(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return safe.Secret{}, ErrNotFound
	}
	if err != nil {
		return safe.Secret{}, err
	}
	return safe.NewSecret(v), nil
}

func (k *keyringBackend) Set(key string, v safe.Secret) error {
	return keyring.Set(service, key, v.Reveal())
}

func (k *keyringBackend) Delete(key string) error {
	err := keyring.Delete(service, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
