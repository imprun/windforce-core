package controlcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultAPIURL    = "http://127.0.0.1:18091"
	defaultWorkspace = "default"
	defaultTokenEnv  = "WINDFORCE_CORE_API_TOKEN"
)

type programConfig struct {
	Name       string
	ConfigDir  string
	ConfigEnv  string
	ContextEnv string
}

var (
	legacyProgram = programConfig{
		Name:       "windforce",
		ConfigDir:  "windforce",
		ConfigEnv:  "WINDFORCE_CONFIG",
		ContextEnv: "WINDFORCE_PROFILE",
	}
	wfProgram = programConfig{
		Name:       "wf",
		ConfigDir:  "wf",
		ConfigEnv:  "WF_CONFIG",
		ContextEnv: "WF_CONTEXT",
	}
)

type Profile struct {
	APIURL    string `json:"api_url,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Actor     string `json:"actor,omitempty"`
	TokenEnv  string `json:"token_env,omitempty"`
}

type ConfigFile struct {
	CurrentProfile string             `json:"current_profile,omitempty"`
	Profiles       map[string]Profile `json:"profiles,omitempty"`
}

type resolvedConfig struct {
	ProfileName string
	Profile
	Token string
}

func configPath() (string, error) {
	return configPathFor(legacyProgram)
}

func configPathFor(program programConfig) (string, error) {
	if path := strings.TrimSpace(os.Getenv(program.ConfigEnv)); path != "" {
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, program.ConfigDir, "config.json"), nil
}

func loadProgramConfig(program programConfig, path string) (ConfigFile, error) {
	if program.Name != wfProgram.Name || strings.TrimSpace(os.Getenv(program.ConfigEnv)) != "" {
		return loadConfig(path)
	}
	if _, err := os.Stat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return loadConfig(path)
	}
	legacyPath, err := configPathFor(legacyProgram)
	if err != nil || legacyPath == path {
		return loadConfig(path)
	}
	return loadConfig(legacyPath)
}

func loadConfig(path string) (ConfigFile, error) {
	config := ConfigFile{Profiles: map[string]Profile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return config, fmt.Errorf("decode config: %w", err)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]Profile{}
	}
	return config, nil
}

func saveConfig(path string, config ConfigFile) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func resolveProfile(config ConfigFile, selected string, overrides Profile) (resolvedConfig, error) {
	return resolveProfileFor(legacyProgram, config, selected, overrides)
}

func resolveProfileFor(program programConfig, config ConfigFile, selected string, overrides Profile) (resolvedConfig, error) {
	contextValues := []string{selected, os.Getenv(program.ContextEnv)}
	if program.Name == wfProgram.Name {
		contextValues = append(contextValues, os.Getenv("WF_PROFILE"), os.Getenv("WINDFORCE_PROFILE"))
	}
	contextValues = append(contextValues, config.CurrentProfile)
	name := firstNonEmpty(contextValues...)
	profile := Profile{}
	if name != "" {
		var ok bool
		profile, ok = config.Profiles[name]
		if !ok {
			return resolvedConfig{}, fmt.Errorf("profile %q does not exist", name)
		}
	}
	apiURLValues := []string{overrides.APIURL}
	workspaceValues := []string{overrides.Workspace}
	actorValues := []string{overrides.Actor}
	tokenEnvValues := []string{overrides.TokenEnv}
	if program.Name == wfProgram.Name {
		apiURLValues = append(apiURLValues, os.Getenv("WF_API_URL"))
		workspaceValues = append(workspaceValues, os.Getenv("WF_WORKSPACE"))
		actorValues = append(actorValues, os.Getenv("WF_ACTOR"))
		tokenEnvValues = append(tokenEnvValues, os.Getenv("WF_TOKEN_ENV"))
	}
	apiURLValues = append(apiURLValues, os.Getenv("WINDFORCE_CORE_API_URL"), os.Getenv("WINDFORCE_LITE_API_URL"), profile.APIURL, defaultAPIURL)
	workspaceValues = append(workspaceValues, os.Getenv("WINDFORCE_CORE_WORKSPACE"), os.Getenv("WINDFORCE_LITE_WORKSPACE"), profile.Workspace, defaultWorkspace)
	actorValues = append(actorValues, os.Getenv("WINDFORCE_CORE_ACTOR"), os.Getenv("WINDFORCE_LITE_ACTOR"), profile.Actor)
	tokenEnvValues = append(tokenEnvValues, os.Getenv("WINDFORCE_CORE_TOKEN_ENV"), profile.TokenEnv, defaultTokenEnv)
	profile.APIURL = firstNonEmpty(apiURLValues...)
	profile.Workspace = firstNonEmpty(workspaceValues...)
	profile.Actor = firstNonEmpty(actorValues...)
	profile.TokenEnv = firstNonEmpty(tokenEnvValues...)
	token := firstNonEmpty(os.Getenv("WF_TOKEN"))
	if program.Name != wfProgram.Name {
		token = ""
	}
	if profile.TokenEnv != "" {
		token = firstNonEmpty(token, os.Getenv(profile.TokenEnv))
	}
	return resolvedConfig{ProfileName: name, Profile: profile, Token: token}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
