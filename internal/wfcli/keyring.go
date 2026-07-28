package wfcli

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const keyringService = "wf"

type credentialStore struct{}

func (credentialStore) Get(key string) (string, bool, error) {
	value, err := keyring.Get(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (credentialStore) Set(key, value string) error {
	return keyring.Set(keyringService, key, value)
}

func (credentialStore) Delete(key string) error {
	err := keyring.Delete(keyringService, key)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
