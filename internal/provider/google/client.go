// Package google is the Google identity provider: one OAuth credential serving
// every Google-backed adapter.
//
// Credentials belong to the provider, not to a service, which is why one
// `connect google` gives you both Gmail and Calendar, why personal and work
// accounts can coexist, and why a future Google service needs no new auth code.
package google

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/safe"
)

// ClientFile is the OAuth client the user downloads from their own Cloud
// project. It is not a true secret — Google states plainly that desktop client
// secrets cannot be kept confidential, which is why PKCE exists — but it is not
// public either, so it is stored 0600.
type ClientFile struct {
	ClientID     string
	ClientSecret string
}

type clientJSON struct {
	Installed struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"installed"`
	Web struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	} `json:"web"`
}

func ClientPath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "google_client.json"), nil
}

// LoadClient reads the downloaded OAuth client, normalising the two shapes
// Google's console produces.
func LoadClient() (ClientFile, error) {
	path, err := ClientPath()
	if err != nil {
		return ClientFile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ClientFile{}, err
	}
	var cj clientJSON
	if err := json.Unmarshal(raw, &cj); err != nil {
		return ClientFile{}, fmt.Errorf("%s is not a Google client JSON file: %w", path, err)
	}
	c := ClientFile{ClientID: cj.Installed.ClientID, ClientSecret: cj.Installed.ClientSecret}
	if c.ClientID == "" {
		c = ClientFile{ClientID: cj.Web.ClientID, ClientSecret: cj.Web.ClientSecret}
	}
	if c.ClientID == "" {
		return ClientFile{}, fmt.Errorf("%s has no client_id — download the JSON for an OAuth client of type \"Desktop app\"", path)
	}
	// Re-assert the mode: the console downloads it world-readable into ~/Downloads
	// and users copy it across with whatever umask they have.
	_ = os.Chmod(path, safe.FileMode)
	return c, nil
}

func ClientExists() bool {
	p, err := ClientPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
