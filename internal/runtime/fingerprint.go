package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/imprun/windforce-core/internal/contract"
	windforcegoclient "github.com/imprun/windforce-core/internal/sdk/go"
	windforcepyclient "github.com/imprun/windforce-core/internal/sdk/python"
	windforceclient "github.com/imprun/windforce-core/internal/sdk/typescript"
)

const sourcePrepareVersion = "prepare-v3"

var sourceRuntimeFingerprints sync.Map

type sourceReadyRecord struct {
	Version  string `json:"version"`
	Language string `json:"language"`
	Runtime  string `json:"runtime"`
	Platform string `json:"platform"`
	SDK      string `json:"sdk"`
}

func sourceReadyValue(ctx context.Context, scriptLang string, pythonPath string, bunPath string, goPath string) (string, error) {
	language, err := contract.NormalizeScriptLanguage(scriptLang)
	if err != nil {
		return "", err
	}
	executable, args := runtimeIdentityCommand(language, pythonPath, bunPath, goPath)
	cacheKey := language + "\x00" + executable
	runtimeIdentity, ok := sourceRuntimeFingerprints.Load(cacheKey)
	if !ok {
		cmd := exec.CommandContext(ctx, executable, args...)
		cmd.Env = curatedPrepareEnv()
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("inspect %s runtime: %w: %s", language, err, strings.TrimSpace(string(output)))
		}
		identity := lastOutputLine(output)
		if identity == "" {
			return "", fmt.Errorf("inspect %s runtime: empty version", language)
		}
		sourceRuntimeFingerprints.Store(cacheKey, identity)
		runtimeIdentity = identity
	}

	sdkDigest, err := sourceSDKDigest(language)
	if err != nil {
		return "", fmt.Errorf("fingerprint %s SDK: %w", language, err)
	}
	record := sourceReadyRecord{
		Version:  sourcePrepareVersion,
		Language: language,
		Runtime:  runtimeIdentity.(string),
		Platform: sourcePlatformIdentity(),
		SDK:      sdkDigest,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ExecutionProfile returns the worker target contract for one app runtime.
// Go releases are built with CGO_ENABLED=0, so their worker ABI is the static
// binary contract rather than the publisher's Go toolchain version.
func (r *Runner) ExecutionProfile(ctx context.Context, scriptLang string) (contract.ExecutionProfile, error) {
	runtimeName, err := contract.NormalizeExecutionRuntime(scriptLang)
	if err != nil {
		return contract.ExecutionProfile{}, err
	}
	runtimeABI := "static-cgo0"
	if runtimeName != contract.ExecutionRuntimeGo {
		language := contract.ScriptLangTypeScript
		executable := firstNonEmpty(r.BunPath, "bun")
		if runtimeName == contract.ExecutionRuntimePython {
			language = contract.ScriptLangPython
			executable = firstNonEmpty(r.PythonPath, defaultPythonPath())
		}
		command, args := runtimeIdentityCommand(language, executable, executable, firstNonEmpty(r.GoPath, "go"))
		cacheKey := "execution-profile\x00" + language + "\x00" + command
		cached, ok := sourceRuntimeFingerprints.Load(cacheKey)
		if !ok {
			cmd := exec.CommandContext(ctx, command, args...)
			cmd.Env = curatedPrepareEnv()
			output, runErr := cmd.CombinedOutput()
			if runErr != nil {
				return contract.ExecutionProfile{}, fmt.Errorf("inspect %s execution runtime: %w: %s", runtimeName, runErr, strings.TrimSpace(string(output)))
			}
			runtimeABI = lastOutputLine(output)
			if runtimeABI == "" {
				return contract.ExecutionProfile{}, fmt.Errorf("inspect %s execution runtime: empty version", runtimeName)
			}
			sourceRuntimeFingerprints.Store(cacheKey, runtimeABI)
		} else {
			runtimeABI = cached.(string)
		}
	}
	libc, err := executionLibcIdentity(ctx, runtimeName)
	if err != nil {
		return contract.ExecutionProfile{}, err
	}
	return contract.NewExecutionProfile(r.ExecutionProfileID, runtime.GOOS, runtime.GOARCH, runtimeName, runtimeABI, libc)
}

var libcVersionPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+`)

func executionLibcIdentity(ctx context.Context, runtimeName string) (string, error) {
	if runtime.GOOS != "linux" || runtimeName == contract.ExecutionRuntimeGo {
		return "none", nil
	}
	if cached, ok := sourceRuntimeFingerprints.Load("execution-profile\x00libc"); ok {
		return cached.(string), nil
	}
	getconf := exec.CommandContext(ctx, "getconf", "GNU_LIBC_VERSION")
	getconf.Env = curatedPrepareEnv()
	if output, err := getconf.CombinedOutput(); err == nil {
		fields := strings.Fields(strings.ToLower(lastOutputLine(output)))
		if len(fields) >= 2 {
			identity := fields[0] + "-" + fields[1]
			sourceRuntimeFingerprints.Store("execution-profile\x00libc", identity)
			return identity, nil
		}
	}
	ldd := exec.CommandContext(ctx, "ldd", "--version")
	ldd.Env = curatedPrepareEnv()
	output, _ := ldd.CombinedOutput() // musl ldd commonly reports its version with a non-zero exit.
	text := strings.ToLower(string(output))
	versions := libcVersionPattern.FindAllString(text, -1)
	if len(versions) > 0 {
		kind := "glibc"
		if strings.Contains(text, "musl") {
			kind = "musl"
		}
		identity := kind + "-" + versions[len(versions)-1]
		sourceRuntimeFingerprints.Store("execution-profile\x00libc", identity)
		return identity, nil
	}
	return "", fmt.Errorf("inspect execution libc: could not identify glibc or musl")
}

func (r *Runner) ExecutionProfiles(ctx context.Context) ([]contract.ExecutionProfile, error) {
	profiles := make([]contract.ExecutionProfile, 0, 3)
	for _, language := range []string{contract.ScriptLangTypeScript, contract.ScriptLangPython, contract.ScriptLangGo} {
		profile, err := r.ExecutionProfile(ctx, language)
		if err != nil {
			// Worker images may intentionally carry only a subset of launchers.
			// Missing Bun/Python runtimes are represented by not advertising the
			// profile; Go execution is a prepared static binary and never probes
			// the worker's Go toolchain.
			continue
		}
		profiles = append(profiles, profile)
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("worker has no detectable execution profiles")
	}
	return profiles, nil
}

func runtimeIdentityCommand(language string, pythonPath string, bunPath string, goPath string) (string, []string) {
	switch language {
	case "python":
		return pythonPath, []string{"-S", "-c", "import struct,sys,sysconfig; print('|'.join((sys.implementation.cache_tag, sys.version.split()[0], sysconfig.get_platform(), str(struct.calcsize('P') * 8))))"}
	case "go":
		return goPath, []string{"version"}
	default:
		return bunPath, []string{"--version"}
	}
}

func sourcePlatformIdentity() string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(runtime.GOOS + "/" + runtime.GOARCH + "\x00" + runtime.Version()))
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		_, _ = hash.Write(data)
	}
	return runtime.GOOS + "/" + runtime.GOARCH + ":" + hex.EncodeToString(hash.Sum(nil))[:16]
}

func sourceSDKDigest(language string) (string, error) {
	hash := sha256.New()
	var files fs.FS
	switch language {
	case "python":
		files = windforcepyclient.Files
	case "go":
		files = windforcegoclient.Files
		_, _ = hash.Write([]byte(wrapperGo()))
	default:
		files = windforceclient.Files
	}
	if err := fs.WalkDir(files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(files, path)
		if readErr != nil {
			return readErr
		}
		_, _ = hash.Write([]byte(filepath.ToSlash(path) + "\x00"))
		_, _ = hash.Write(data)
		return nil
	}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
