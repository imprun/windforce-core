package runtime

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"

	windforcepyclient "github.com/imprun/windforce-lite/internal/sdk/python"
	windforceclient "github.com/imprun/windforce-lite/internal/sdk/typescript"
)

const pyVendorDir = ".windforce/site-packages"

func (r *Runner) prepareSource(ctx context.Context, sourceDir string, scriptLang string) error {
	switch scriptLang {
	case "", "typescript":
		if fileExists(filepath.Join(sourceDir, "package.json")) {
			if err := bunInstall(ctx, firstNonEmpty(r.BunPath, "bun"), sourceDir); err != nil {
				return fmt.Errorf("bun install: %w", err)
			}
		}
		if err := injectTypeScriptSDK(sourceDir); err != nil {
			return fmt.Errorf("inject sdk: %w", err)
		}
	case "python":
		if fileExists(filepath.Join(sourceDir, "requirements.txt")) {
			if err := pythonInstall(ctx, firstNonEmpty(r.PythonPath, defaultPythonPath()), sourceDir); err != nil {
				return fmt.Errorf("pip install: %w", err)
			}
		}
		if err := injectPythonSDK(sourceDir); err != nil {
			return fmt.Errorf("inject python sdk: %w", err)
		}
	}
	return nil
}

func bunInstall(ctx context.Context, bunPath string, dir string) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, bunPath, "install", "--frozen-lockfile", "--no-progress")
	cmd.Dir = dir
	cmd.Env = curatedHostEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func injectTypeScriptSDK(dir string) error {
	target := filepath.Join(dir, "node_modules", "windforce-client")
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"index.ts", "index.d.ts", "package.json"} {
		data, err := windforceclient.Files.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func pythonInstall(ctx context.Context, pythonPath string, dir string) error {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, pythonPath, "-m", "pip", "install",
		"--target", filepath.Join(dir, pyVendorDir),
		"--no-input", "--disable-pip-version-check",
		"-r", filepath.Join(dir, "requirements.txt"))
	cmd.Dir = dir
	cmd.Env = curatedHostEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

func injectPythonSDK(dir string) error {
	target := filepath.Join(dir, pyVendorDir)
	return fs.WalkDir(windforcepyclient.Files, "windforce_client", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(target, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := windforcepyclient.Files.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}

func appendPreparedSourceEnv(env []string, sourceDir string, scriptLang string) []string {
	if scriptLang == "python" {
		return append(env, "WF_PY_VENDOR="+filepath.Join(sourceDir, pyVendorDir))
	}
	return env
}

func defaultPythonPath() string {
	if goruntime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
