package pythonruntime

import (
	"os"
	"path/filepath"
	"strings"
)

// FindWindowsExecutable returns the first real Python interpreter on PATH.
// WindowsApps entries are execution aliases, not stable interpreter paths:
// invoking one from a prepared source directory may bootstrap Python into that
// directory and contaminate the execution bundle.
func FindWindowsExecutable(pathValue string) string {
	for _, dir := range filepath.SplitList(pathValue) {
		dir = strings.Trim(strings.TrimSpace(dir), `"`)
		if dir == "" {
			continue
		}
		for _, name := range []string{"python.exe", "python3.exe"} {
			candidate := filepath.Join(dir, name)
			if IsWindowsAppsAlias(candidate) {
				continue
			}
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				if path, absErr := filepath.Abs(candidate); absErr == nil {
					return path
				}
				return candidate
			}
		}
	}
	return ""
}

func IsWindowsAppsAlias(path string) bool {
	clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return strings.Contains(clean, "/microsoft/windowsapps/")
}
