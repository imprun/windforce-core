package controlcli

import (
	"fmt"
	"net/url"
	"strings"
)

type CredentialStore interface {
	Get(key string) (value string, found bool, err error)
	Set(key, value string) error
	Delete(key string) error
}

func credentialKey(profile Profile) (string, error) {
	host, err := credentialHost(profile.APIURL)
	if err != nil {
		return "", err
	}
	account := strings.TrimSpace(profile.Account)
	if account == "" {
		return "", fmt.Errorf("context has no authenticated account")
	}
	return host + "/" + account, nil
}

func credentialHost(apiURL string) (string, error) {
	target, err := url.Parse(strings.TrimSpace(apiURL))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return "", fmt.Errorf("invalid context API URL %q", apiURL)
	}
	return strings.ToLower(target.Host), nil
}
