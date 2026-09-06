package secrets

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/safe"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
)

// PassphraseFunc obtains the passphrase that seals the file backend.
// confirm asks the caller to prompt twice when creating a new store.
type PassphraseFunc func(prompt string, confirm bool) (safe.Secret, error)

const (
	fileMagic   = "commhub-secrets-v1"
	saltLen     = 16
	nonceLen    = 24
	argonTime   = 3
	argonMemory = 64 * 1024
	argonPar    = 4
)

type sealedFile struct {
	Magic string `json:"magic"`
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	Box   []byte `json:"box"`
}

// fileBackend keeps an argon2id-sealed map of secrets next to the config.
// The passphrase is requested once and held for the process lifetime only.
type fileBackend struct {
	prompt PassphraseFunc
	path   string
	key    *[32]byte
	data   map[string]string
	salt   []byte
	loaded bool
}

func newFileBackend(prompt PassphraseFunc) *fileBackend {
	path := ""
	if dir, err := config.Dir(); err == nil {
		path = filepath.Join(dir, "secrets.sealed")
	}
	return &fileBackend{prompt: prompt, path: path, data: map[string]string{}}
}

func (f *fileBackend) Name() string { return "file" }

// Available requires a place to put the file and a way to ask for a
// passphrase. Without a prompt (non-interactive runs) this backend is unusable.
func (f *fileBackend) Available() bool { return f.path != "" && f.prompt != nil }

func (f *fileBackend) derive(pass safe.Secret, salt []byte) *[32]byte {
	sum := argon2.IDKey([]byte(pass.Reveal()), salt, argonTime, argonMemory, argonPar, 32)
	var k [32]byte
	copy(k[:], sum)
	return &k
}

func (f *fileBackend) load() error {
	if f.loaded {
		return nil
	}
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		pass, err := f.prompt("Create a passphrase for CommHub's secret store", true)
		if err != nil {
			return err
		}
		salt := make([]byte, saltLen)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return err
		}
		f.key = f.derive(pass, salt)
		f.data = map[string]string{}
		f.loaded = true
		return f.flushWithSalt(salt)
	}
	if err != nil {
		return err
	}
	var sf sealedFile
	if err := json.Unmarshal(raw, &sf); err != nil || sf.Magic != fileMagic {
		return fmt.Errorf("secrets: %s is not a CommHub secret store", f.path)
	}
	pass, err := f.prompt("Passphrase for CommHub's secret store", false)
	if err != nil {
		return err
	}
	f.key = f.derive(pass, sf.Salt)

	var nonce [nonceLen]byte
	copy(nonce[:], sf.Nonce)
	plain, ok := secretbox.Open(nil, sf.Box, &nonce, f.key)
	if !ok {
		return errors.New("secrets: wrong passphrase")
	}
	if err := json.Unmarshal(plain, &f.data); err != nil {
		return err
	}
	f.salt = sf.Salt
	f.loaded = true
	return nil
}

func (f *fileBackend) flush() error { return f.flushWithSalt(f.salt) }

func (f *fileBackend) flushWithSalt(salt []byte) error {
	f.salt = salt
	plain, err := json.Marshal(f.data)
	if err != nil {
		return err
	}
	var nonce [nonceLen]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return err
	}
	sf := sealedFile{
		Magic: fileMagic,
		Salt:  salt,
		Nonce: nonce[:],
		Box:   secretbox.Seal(nil, plain, &nonce, f.key),
	}
	out, err := json.Marshal(sf)
	if err != nil {
		return err
	}
	return safe.WriteFileSecure(f.path, out)
}

func (f *fileBackend) Get(key string) (safe.Secret, error) {
	if err := f.load(); err != nil {
		return safe.Secret{}, err
	}
	v, ok := f.data[key]
	if !ok {
		return safe.Secret{}, ErrNotFound
	}
	return safe.NewSecret(v), nil
}

func (f *fileBackend) Set(key string, v safe.Secret) error {
	if err := f.load(); err != nil {
		return err
	}
	f.data[key] = v.Reveal()
	return f.flush()
}

func (f *fileBackend) Delete(key string) error {
	if err := f.load(); err != nil {
		return err
	}
	delete(f.data, key)
	return f.flush()
}
