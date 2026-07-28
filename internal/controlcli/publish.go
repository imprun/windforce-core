package controlcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"

	"github.com/imprun/windforce-core/internal/manifest"
)

type publishTarget struct {
	App          string
	Branch       string
	Commit       string
	Dirty        bool
	ManifestPath string
	RemoteName   string
	RepoRoot     string
	RepoURL      string
	RepoKey      string
	Subpath      string
}

type publishGitSource struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
	Subpath     string `json:"subpath"`
	CredsRef    string `json:"creds_ref"`
}

type publishSyncResult struct {
	App    string `json:"app"`
	Commit string `json:"commit"`
}

func (r *runner) appPublish(args []string) error {
	fs := r.flags("app publish")
	sourceIDText := fs.String("source-id", "", "use an existing Git source ID")
	sourceName := fs.String("source-name", "", "select or register a Git source by name")
	credsRef := fs.String("creds-ref", "", "server-side credential reference for a new private Git source")
	remote := fs.String("remote", "", "Git remote name")
	branch := fs.String("branch", "", "remote branch")
	message := fs.String("message", "", "release audit message")
	allowDirty := fs.Bool("allow-dirty", false, "publish HEAD while ignoring uncommitted changes")
	quiet := fs.Bool("quiet", false, "suppress progress messages")

	pathArg := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pathArg = args[0]
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() > 1 || (pathArg != "." && fs.NArg() != 0) {
		return usageError{fmt.Sprintf("usage: %s app publish [path] [flags]", r.program)}
	}
	if fs.NArg() == 1 {
		pathArg = fs.Arg(0)
	}
	if strings.TrimSpace(*sourceIDText) != "" && strings.TrimSpace(*sourceName) != "" {
		return usageError{"--source-id and --source-name cannot be used together"}
	}

	var sourceID int64
	if value := strings.TrimSpace(*sourceIDText); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return usageError{"--source-id must be a positive integer"}
		}
		sourceID = parsed
	}

	target, err := inspectPublishTarget(pathArg, *remote, *branch, *allowDirty)
	if err != nil {
		return err
	}
	r.publishProgress(*quiet, "Resolved %s at %s", target.App, shortCommit(target.Commit))
	if target.Dirty {
		r.publishProgress(*quiet, "Ignoring uncommitted changes; only HEAD will be published")
	}

	sources, err := r.listPublishGitSources()
	if err != nil {
		return err
	}
	source, registered, err := r.resolvePublishGitSource(
		sources,
		target,
		sourceID,
		strings.TrimSpace(*sourceName),
		strings.TrimSpace(*credsRef),
		*quiet,
	)
	if err != nil {
		return err
	}

	r.publishProgress(*quiet, "Synchronizing source %d", source.ID)
	syncRaw, err := r.client.DoJSON(
		context.Background(),
		http.MethodPost,
		r.client.WorkspacePath("git_sources", strconv.FormatInt(source.ID, 10), "sync"),
		map[string]string{"expected_commit": target.Commit},
	)
	if err != nil {
		return err
	}
	var syncResult publishSyncResult
	if err := json.Unmarshal(syncRaw, &syncResult); err != nil {
		return fmt.Errorf("decode source synchronization result: %w", err)
	}
	if syncResult.Commit != target.Commit {
		return fmt.Errorf("Cell synchronized an unexpected commit; expected %s", shortCommit(target.Commit))
	}
	if syncResult.App != target.App {
		return fmt.Errorf("Cell synchronized app %q instead of manifest app %q", syncResult.App, target.App)
	}

	r.publishProgress(*quiet, "Publishing immutable release")
	deployRaw, err := r.client.DoJSON(
		context.Background(),
		http.MethodPost,
		r.client.WorkspacePath("git_sources", strconv.FormatInt(source.ID, 10), "deploy"),
		sourceDeployRequest{
			Confirm:        true,
			Message:        strings.TrimSpace(*message),
			ExpectedCommit: target.Commit,
		},
	)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := json.Unmarshal(deployRaw, &result); err != nil {
		return fmt.Errorf("decode release publication result: %w", err)
	}
	if commit, _ := result["commit"].(string); commit != target.Commit {
		return fmt.Errorf("Cell published an unexpected commit; expected %s", shortCommit(target.Commit))
	}
	if app, _ := result["app"].(string); app != target.App {
		return fmt.Errorf("Cell published app %q instead of manifest app %q", app, target.App)
	}
	if releaseID, _ := result["release_id"].(string); strings.TrimSpace(releaseID) == "" {
		return errors.New("Cell published the release without returning its immutable release ID")
	}
	result["source_id"] = source.ID
	result["source_name"] = source.Name
	result["registered"] = registered
	result["workspace"] = r.resolved.Workspace
	result["manifest"] = target.ManifestPath
	if r.resolved.ProfileName != "" {
		result["context"] = r.resolved.ProfileName
	}
	if target.Dirty {
		result["dirty_changes_ignored"] = true
	}
	r.publishProgress(*quiet, "Published %s at %s", target.App, shortCommit(target.Commit))
	return r.outputJSON(result)
}

func (r *runner) listPublishGitSources() ([]publishGitSource, error) {
	raw, err := r.client.DoJSON(
		context.Background(),
		http.MethodGet,
		r.client.WorkspacePath("git_sources"),
		nil,
	)
	if err != nil {
		return nil, err
	}
	var sources []publishGitSource
	if err := json.Unmarshal(raw, &sources); err != nil {
		return nil, fmt.Errorf("decode Git source list: %w", err)
	}
	return sources, nil
}

func (r *runner) resolvePublishGitSource(
	sources []publishGitSource,
	target publishTarget,
	sourceID int64,
	sourceName string,
	credsRef string,
	quiet bool,
) (publishGitSource, bool, error) {
	if sourceID > 0 {
		for _, source := range sources {
			if source.ID != sourceID {
				continue
			}
			if err := sourceMatchesPublishTarget(source, target); err != nil {
				return publishGitSource{}, false, fmt.Errorf("Git source %d does not match this checkout: %w", sourceID, err)
			}
			return source, false, nil
		}
		return publishGitSource{}, false, fmt.Errorf("Git source %d was not found in workspace %q", sourceID, r.resolved.Workspace)
	}

	matches := make([]publishGitSource, 0, 1)
	for _, source := range sources {
		if sourceName != "" && source.Name != sourceName {
			continue
		}
		if sourceMatchesPublishTarget(source, target) == nil {
			matches = append(matches, source)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], false, nil
	case 0:
	default:
		return publishGitSource{}, false, fmt.Errorf("multiple Git sources match this checkout; select one with --source-id")
	}

	name := sourceName
	if name == "" {
		name = target.App
	}
	for _, source := range sources {
		if source.Name == name {
			return publishGitSource{}, false, fmt.Errorf("Git source name %q already refers to a different checkout; use --source-name", name)
		}
	}

	r.publishProgress(quiet, "Registering Git source %s", name)
	raw, err := r.client.DoJSON(
		context.Background(),
		http.MethodPost,
		r.client.WorkspacePath("git_sources"),
		map[string]string{
			"name":      name,
			"repo_url":  target.RepoURL,
			"branch":    target.Branch,
			"subpath":   target.Subpath,
			"creds_ref": credsRef,
		},
	)
	if err != nil {
		return publishGitSource{}, false, err
	}
	var source publishGitSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return publishGitSource{}, false, fmt.Errorf("decode registered Git source: %w", err)
	}
	if source.ID <= 0 {
		return publishGitSource{}, false, fmt.Errorf("Cell returned an invalid Git source ID")
	}
	if err := sourceMatchesPublishTarget(source, target); err != nil {
		return publishGitSource{}, false, fmt.Errorf("Cell registered a mismatched Git source: %w", err)
	}
	return source, true, nil
}

func sourceMatchesPublishTarget(source publishGitSource, target publishTarget) error {
	sourceKey, err := normalizeRepoURL(source.RepoURL, target.RepoRoot)
	if err != nil {
		return fmt.Errorf("source repository URL is invalid")
	}
	if sourceKey != target.RepoKey {
		return fmt.Errorf("repository differs")
	}
	if strings.TrimPrefix(strings.TrimSpace(source.Branch), "refs/heads/") != target.Branch {
		return fmt.Errorf("branch differs")
	}
	if normalizeRepoSubpath(source.Subpath) != normalizeRepoSubpath(target.Subpath) {
		return fmt.Errorf("subpath differs")
	}
	return nil
}

func inspectPublishTarget(pathArg string, remoteOverride string, branchOverride string, allowDirty bool) (publishTarget, error) {
	start, err := filepath.Abs(pathArg)
	if err != nil {
		return publishTarget{}, fmt.Errorf("resolve publish path: %w", err)
	}
	info, err := os.Stat(start)
	if err != nil {
		return publishTarget{}, fmt.Errorf("inspect publish path: %w", err)
	}
	if !info.IsDir() {
		if filepath.Base(start) != manifest.FileName {
			return publishTarget{}, fmt.Errorf("publish path must be a directory or %s", manifest.FileName)
		}
		start = filepath.Dir(start)
	}

	repoRootText, err := gitOutput(start, "rev-parse", "--show-toplevel")
	if err != nil {
		return publishTarget{}, fmt.Errorf("publish path is not inside a Git worktree")
	}
	repoRoot, err := filepath.Abs(filepath.Clean(repoRootText))
	if err != nil {
		return publishTarget{}, fmt.Errorf("resolve Git worktree root: %w", err)
	}
	manifestDir, err := nearestManifestDir(start, repoRoot)
	if err != nil {
		return publishTarget{}, err
	}
	subpath, err := filepath.Rel(repoRoot, manifestDir)
	if err != nil || strings.HasPrefix(subpath, "..") {
		return publishTarget{}, fmt.Errorf("manifest is outside the Git worktree")
	}
	subpath = normalizeRepoSubpath(subpath)
	manifestPath := manifest.FileName
	if subpath != "" {
		manifestPath = subpath + "/" + manifest.FileName
	}

	status, err := gitOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return publishTarget{}, fmt.Errorf("inspect Git worktree status: %w", err)
	}
	dirty := strings.TrimSpace(status) != ""
	if dirty && !allowDirty {
		return publishTarget{}, fmt.Errorf("Git worktree has uncommitted changes; commit them or pass --allow-dirty to publish HEAD only")
	}
	commit, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil || !validGitCommit(commit) {
		return publishTarget{}, fmt.Errorf("resolve current Git commit")
	}
	appManifest, err := manifest.Load(manifestDir)
	if dirty && allowDirty {
		committedManifest, showErr := gitOutput(repoRoot, "show", "HEAD:"+manifestPath)
		if showErr != nil {
			return publishTarget{}, fmt.Errorf("read committed %s from HEAD", manifestPath)
		}
		appManifest, err = manifest.Parse([]byte(committedManifest))
	}
	if err != nil {
		return publishTarget{}, err
	}

	localBranch, attached := optionalGitOutput(repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	upstream, hasUpstream := optionalGitOutput(repoRoot, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	upstreamRemote, upstreamBranch := splitUpstream(upstream)

	remoteName := strings.TrimSpace(remoteOverride)
	if remoteName == "" && hasUpstream {
		remoteName = upstreamRemote
	}
	if remoteName == "" {
		remoteName = "origin"
	}
	branch := strings.TrimPrefix(strings.TrimSpace(branchOverride), "refs/heads/")
	if branch == "" && hasUpstream {
		branch = upstreamBranch
	}
	if branch == "" && attached {
		branch = localBranch
	}
	if branch == "" {
		return publishTarget{}, fmt.Errorf("detached HEAD requires an explicit --branch")
	}
	if _, err := gitOutput(repoRoot, "check-ref-format", "--branch", branch); err != nil {
		return publishTarget{}, fmt.Errorf("invalid remote branch")
	}

	repoURL, err := gitOutput(repoRoot, "remote", "get-url", remoteName)
	if err != nil {
		return publishTarget{}, fmt.Errorf("Git remote %q was not found", remoteName)
	}
	repoKey, err := normalizeRepoURL(repoURL, repoRoot)
	if err != nil {
		return publishTarget{}, err
	}
	if trackedCommit, ok := optionalGitOutput(repoRoot, "rev-parse", "--verify", "refs/remotes/"+remoteName+"/"+branch); ok &&
		validGitCommit(trackedCommit) && trackedCommit != commit {
		return publishTarget{}, fmt.Errorf("HEAD is not the tracked tip of %s/%s; push the commit before publishing", remoteName, branch)
	}

	return publishTarget{
		App:          appManifest.App,
		Branch:       branch,
		Commit:       commit,
		Dirty:        dirty,
		ManifestPath: manifestPath,
		RemoteName:   remoteName,
		RepoRoot:     repoRoot,
		RepoURL:      repoURL,
		RepoKey:      repoKey,
		Subpath:      subpath,
	}, nil
}

func nearestManifestDir(start string, repoRoot string) (string, error) {
	current := filepath.Clean(start)
	root := filepath.Clean(repoRoot)
	for {
		if _, err := os.Stat(filepath.Join(current, manifest.FileName)); err == nil {
			return current, nil
		}
		if samePath(current, root) {
			break
		}
		parent := filepath.Dir(current)
		if samePath(parent, current) {
			break
		}
		current = parent
	}
	return "", fmt.Errorf("no %s found between publish path and Git worktree root", manifest.FileName)
}

func normalizeRepoURL(raw string, base string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("Git repository URL is empty")
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("Git repository URL contains control characters")
	}
	if filepath.VolumeName(value) != "" || filepath.IsAbs(value) {
		return normalizeLocalRepo(value, base)
	}

	if isSCPGitURL(value) {
		index := strings.Index(value, ":")
		hostPart := value[:index]
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		pathPart := value[index+1:]
		return normalizeHostedRepo(hostPart, pathPart)
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("Git repository URL is invalid")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("Git repository URL must not contain a query or fragment")
	}
	if parsed.Scheme != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			if parsed.User != nil {
				return "", fmt.Errorf("Git repository URL must not contain embedded credentials; use --creds-ref")
			}
			return normalizeHostedRepo(parsed.Host, parsed.Path)
		case "ssh", "git", "git+ssh":
			if parsed.User != nil {
				if _, hasPassword := parsed.User.Password(); hasPassword {
					return "", fmt.Errorf("Git repository URL must not contain an embedded password; use --creds-ref")
				}
			}
			return normalizeHostedRepo(parsed.Host, parsed.Path)
		case "file":
			if parsed.User != nil {
				return "", fmt.Errorf("file repository URL must not contain user information")
			}
			local := filepath.FromSlash(parsed.Path)
			if runtime.GOOS == "windows" && len(local) >= 3 && local[0] == '\\' && local[2] == ':' {
				local = local[1:]
			}
			return normalizeLocalRepo(local, base)
		default:
			return "", fmt.Errorf("unsupported Git repository URL scheme")
		}
	}
	return normalizeLocalRepo(value, base)
}

func normalizeHostedRepo(host string, path string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
	path = strings.TrimSuffix(path, ".git")
	if host == "" || path == "" || pathHasParentSegment(path) {
		return "", fmt.Errorf("Git repository URL is invalid")
	}
	return "host:" + host + "/" + path, nil
}

func pathHasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

func normalizeLocalRepo(path string, base string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve local Git repository URL: %w", err)
	}
	value := filepath.ToSlash(strings.TrimSuffix(path, ".git"))
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return "file:" + value, nil
}

func isSCPGitURL(value string) bool {
	if strings.Contains(value, "://") || filepath.VolumeName(value) != "" {
		return false
	}
	index := strings.Index(value, ":")
	if index <= 0 {
		return false
	}
	host := value[:index]
	return !strings.ContainsAny(host, `/\`)
}

func normalizeRepoSubpath(value string) string {
	value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
	if value == "." || value == "/" {
		return ""
	}
	return strings.Trim(value, "/")
}

func splitUpstream(value string) (string, string) {
	value = strings.TrimSpace(value)
	index := strings.Index(value, "/")
	if index <= 0 || index == len(value)-1 {
		return "", ""
	}
	return value[:index], value[index+1:]
}

func validGitCommit(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 40 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

func gitOutput(dir string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", commandArgs...).Output()
	if err != nil {
		if _, ok := err.(*exec.Error); ok {
			return "", fmt.Errorf("git executable is required")
		}
		return "", fmt.Errorf("git %s failed", firstGitOperation(args))
	}
	return strings.TrimSpace(string(output)), nil
}

func optionalGitOutput(dir string, args ...string) (string, bool) {
	value, err := gitOutput(dir, args...)
	return value, err == nil
}

func firstGitOperation(args []string) string {
	if len(args) == 0 {
		return "command"
	}
	return args[0]
}

func samePath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}

func (r *runner) publishProgress(quiet bool, format string, args ...any) {
	if quiet {
		return
	}
	_, _ = fmt.Fprintf(r.stderr, format+"\n", args...)
}
