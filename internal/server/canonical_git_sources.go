package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/imprun/windforce-core/internal/contract"
	gitsourcepkg "github.com/imprun/windforce-core/internal/gitsource"
	"github.com/imprun/windforce-core/internal/sampleapp"
	sourcepkg "github.com/imprun/windforce-core/internal/source"
)

type gitCredentialRequest struct {
	AuthMethod  string
	AccessToken string
	Username    string
	Password    string
}

type canonicalGitSourceDeployRequest struct {
	Confirm        bool    `json:"confirm"`
	Confirmed      bool    `json:"confirmed"`
	ConfirmCamel   bool    `json:"Confirm"`
	ConfirmedCamel bool    `json:"Confirmed"`
	Message        *string `json:"message"`
	MessageCamel   *string `json:"Message"`
	Commit         string  `json:"commit"`
	CommitCamel    string  `json:"Commit"`
	ExpectedCommit string  `json:"expected_commit"`
	ExpectedCamel  string  `json:"expectedCommit"`
}

type canonicalGitSourceSyncRequest struct {
	ExpectedCommit string `json:"expected_commit"`
	ExpectedCamel  string `json:"expectedCommit"`
}

const (
	gitSourceErrorBranchNotFound        = "git_source_branch_not_found"
	gitSourceErrorCredentialUnavailable = "git_source_credential_unavailable"
	gitSourceErrorPlacementAfterSync    = "git_source_placement_requires_sync"
	gitSourceErrorRepositoryUnreachable = "git_source_repository_unreachable"
	gitSourceErrorSubpathInvalid        = "git_source_subpath_invalid"
)

func (h *Handler) handleCanonicalGitSources(w http.ResponseWriter, r *http.Request, workspaceID string) {
	snapshot, ok := h.loadGitSourceSnapshot(w, r)
	if !ok {
		return
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	items := make([]canonicalGitSourceView, 0, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		if contract.NormalizeWorkspace(source.Workspace) != workspaceID {
			continue
		}
		items = append(items, newCanonicalGitSourceView(source))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ID == items[j].ID {
			return items[i].Name < items[j].Name
		}
		return items[i].ID < items[j].ID
	})
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) handleCanonicalRegisterGitSource(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.gitSources == nil {
		writeError(w, http.StatusServiceUnavailable, "git source registry is not configured")
		return
	}
	var request struct {
		Name     string `json:"name"`
		RepoURL  string `json:"repo_url"`
		Branch   string `json:"branch"`
		Subpath  string `json:"subpath"`
		CredsRef string `json:"creds_ref"`

		AuthMethod  string `json:"auth_method"`
		AccessToken string `json:"access_token"`
		Username    string `json:"username"`
		Password    string `json:"password"`

		NameCamel     string `json:"Name"`
		RepoURLCamel  string `json:"RepoURL"`
		BranchCamel   string `json:"Branch"`
		SubpathCamel  string `json:"Subpath"`
		CredsRefCamel string `json:"CredsRef"`

		PlacementPolicy *canonicalRoutingPolicyRequest `json:"placement_policy"`
	}
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "name and repo_url required")
		return
	}
	name := strings.TrimSpace(firstNonEmpty(request.Name, request.NameCamel))
	repoURL := strings.TrimSpace(firstNonEmpty(request.RepoURL, request.RepoURLCamel))
	branch := strings.TrimSpace(firstNonEmpty(request.Branch, request.BranchCamel))
	subpath := strings.TrimSpace(firstNonEmpty(request.Subpath, request.SubpathCamel))
	credsRef := strings.TrimSpace(firstNonEmpty(request.CredsRef, request.CredsRefCamel))
	if gitSourceRegisterHasInlineCredential(request.AuthMethod, request.AccessToken, request.Username, request.Password) {
		writeError(w, http.StatusBadRequest, "git credentials must be stored as a secret variable and referenced by creds_ref")
		return
	}
	if name == "" || repoURL == "" {
		writeError(w, http.StatusBadRequest, "name and repo_url required")
		return
	}
	if request.PlacementPolicy != nil {
		writeGitSourceError(
			w,
			http.StatusUnprocessableEntity,
			gitSourceErrorPlacementAfterSync,
			"execution placement can be configured after the source has been synchronized",
		)
		return
	}
	if branch == "" {
		branch = "main"
	}
	source := gitsourcepkg.Source{
		Workspace: workspaceID,
		Name:      name,
		RepoURL:   repoURL,
		Branch:    branch,
		Subpath:   subpath,
		TokenEnv:  credsRef,
	}
	if err := contract.ValidateSourceSubpath(subpath); err != nil {
		writeGitSourceError(w, http.StatusBadRequest, gitSourceErrorSubpathInvalid, "subpath must be a relative path inside the repository")
		return
	}
	validationToken, err := h.resolveGitSourceCreds(r.Context(), workspaceID, credsRef)
	if err != nil {
		writeGitSourceError(w, http.StatusUnprocessableEntity, gitSourceErrorCredentialUnavailable, "Git credential could not be loaded")
		return
	}
	if !h.requireGitSourceAccess(w, r, source, validationToken) {
		return
	}
	created, ok := h.createGitSource(w, r, source)
	if !ok {
		return
	}
	writeJSON(w, http.StatusCreated, newCanonicalGitSourceView(created))
}

func (h *Handler) createGitSource(w http.ResponseWriter, r *http.Request, source gitsourcepkg.Source) (gitsourcepkg.Source, bool) {
	if creator, ok := h.gitSources.(interface {
		Create(context.Context, gitsourcepkg.Source) (gitsourcepkg.Source, error)
	}); ok {
		created, err := creator.Create(r.Context(), source)
		if errors.Is(err, gitsourcepkg.ErrGitSourceConflict) {
			writeError(w, http.StatusConflict, "git source name already exists")
			return gitsourcepkg.Source{}, false
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return gitsourcepkg.Source{}, false
		}
		h.recordAudit(r, created.Workspace, created.ID, "", "source_registered", gitSourceAuditDetail(created))
		return created, true
	}

	if _, err := h.gitSources.Get(r.Context(), source.Workspace, source.ID); err == nil {
		writeError(w, http.StatusConflict, "git source name already exists")
		return gitsourcepkg.Source{}, false
	} else if !errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return gitsourcepkg.Source{}, false
	}
	if err := h.gitSources.Upsert(r.Context(), source); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return gitsourcepkg.Source{}, false
	}
	source, err := h.gitSources.Get(r.Context(), source.Workspace, source.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return gitsourcepkg.Source{}, false
	}
	h.recordAudit(r, source.Workspace, source.ID, "", "source_registered", gitSourceAuditDetail(source))
	return source, true
}

func (h *Handler) handleCanonicalProbeGitSource(w http.ResponseWriter, r *http.Request, workspaceID string) {
	var request struct {
		RepoURL     string `json:"repo_url"`
		Branch      string `json:"branch"`
		AuthMethod  string `json:"auth_method"`
		AccessToken string `json:"access_token"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		CredsRef    string `json:"creds_ref"`
		Subpath     string `json:"subpath"`
	}
	if err := readOptionalJSON(r, &request); err != nil || strings.TrimSpace(request.RepoURL) == "" {
		writeError(w, http.StatusBadRequest, "repo_url required")
		return
	}
	token, err := gitCredentialFromRequest(gitCredentialRequest{
		AuthMethod:  request.AuthMethod,
		AccessToken: request.AccessToken,
		Username:    request.Username,
		Password:    request.Password,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if token == "" {
		resolved, err := h.resolveGitSourceCreds(r.Context(), workspaceID, request.CredsRef)
		if err != nil {
			writeGitSourceError(w, http.StatusUnprocessableEntity, gitSourceErrorCredentialUnavailable, "Git credential could not be loaded")
			return
		}
		token = resolved
	}
	if err := contract.ValidateSourceSubpath(strings.TrimSpace(request.Subpath)); err != nil {
		writeGitSourceError(w, http.StatusBadRequest, gitSourceErrorSubpathInvalid, "subpath must be a relative path inside the repository")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
	defer cancel()
	branches, err := sourcepkg.ListRemoteBranches(ctx, strings.TrimSpace(request.RepoURL), token)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"reachable": false,
			"code":      gitSourceErrorRepositoryUnreachable,
			"error":     "repository cannot be reached with the provided Git credential",
			"branches":  []string{},
		})
		return
	}
	branch := strings.TrimSpace(request.Branch)
	if branch == "" {
		branch = "main"
	}
	branchExists := stringSliceContains(branches, branch)
	result := map[string]any{
		"reachable":     true,
		"branch":        branch,
		"branch_exists": branchExists,
		"branches":      branches,
	}
	if !branchExists {
		result["code"] = gitSourceErrorBranchNotFound
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) handleCanonicalSampleGitSource(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if h.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "sync API is not configured")
		return
	}
	if h.gitSources == nil {
		writeError(w, http.StatusServiceUnavailable, "git source registry is not configured")
		return
	}
	var request struct {
		AppKey string `json:"app_key"`
	}
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	repo, err := sampleapp.EnsureRepository(r.Context(), h.sampleRoot, workspaceID, request.AppKey, h.manifestFileName())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	source := gitsourcepkg.Source{
		Workspace: workspaceID,
		Name:      repo.SourceName,
		RepoURL:   repo.RepoURL,
		Branch:    repo.Branch,
		Kind:      "managed",
	}
	status := http.StatusCreated
	existing, err := h.gitSources.Get(r.Context(), workspaceID, repo.SourceName)
	if err == nil {
		source.CreatedAt = existing.CreatedAt
		source.LastSyncedCommit = existing.LastSyncedCommit
		source.LastSyncedAt = existing.LastSyncedAt
		if err := h.gitSources.Upsert(r.Context(), source); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		source, err = h.gitSources.Get(r.Context(), workspaceID, repo.SourceName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status = http.StatusOK
	} else if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
		created, ok := h.createGitSource(w, r, source)
		if !ok {
			return
		}
		source = created
	} else {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, ok := h.syncGitSourceRevision(w, r, workspaceID, source, "")
	if !ok {
		return
	}
	publication, ok := h.deployLatestGitSourceRevision(w, r, workspaceID, source, "", gitSourceOperationAudit{
		Source: "external_sync",
	})
	if !ok {
		return
	}
	writeJSON(w, status, map[string]any{
		"source":      newCanonicalGitSourceView(source),
		"sync_result": newCanonicalDeployResult(publication.Deployment, publication.ReleaseID),
	})
}

func (h *Handler) handleCanonicalPatchGitSource(w http.ResponseWriter, r *http.Request, workspaceID string, sourceID string) {
	var ok bool
	sourceID, ok = requireCanonicalGitSourceRouteID(w, sourceID)
	if !ok {
		return
	}
	patcher, ok := h.gitSources.(interface {
		Patch(context.Context, string, string, gitsourcepkg.Patch) (gitsourcepkg.Source, error)
	})
	if !ok {
		writeError(w, http.StatusNotImplemented, "git source patch is not supported")
		return
	}
	var request canonicalGitSourcePatchRequest
	if err := readRequiredJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	patch, ok := canonicalGitSourcePatchFromRequest(w, request)
	if !ok {
		return
	}
	existing, err := h.gitSources.Get(r.Context(), workspaceID, sourceID)
	if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
		writeError(w, http.StatusNotFound, "git source not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	candidate := applyGitSourcePatch(existing, patch)
	if existing.LastSyncedCommit != nil &&
		(candidate.RepoURL != existing.RepoURL || candidate.Subpath != existing.Subpath) {
		writeError(w, http.StatusConflict, "repository URL and subpath are locked after the first release")
		return
	}
	if err := contract.ValidateSourceSubpath(candidate.Subpath); err != nil {
		writeGitSourceError(w, http.StatusBadRequest, gitSourceErrorSubpathInvalid, "subpath must be a relative path inside the repository")
		return
	}
	if gitSourcePatchRequiresAccessCheck(existing, candidate) {
		token, err := h.resolveGitSourceCreds(r.Context(), workspaceID, candidate.TokenEnv)
		if err != nil {
			writeGitSourceError(w, http.StatusUnprocessableEntity, gitSourceErrorCredentialUnavailable, "Git credential could not be loaded")
			return
		}
		if !h.requireGitSourceAccess(w, r, candidate, token) {
			return
		}
	}
	source, err := patcher.Patch(r.Context(), workspaceID, sourceID, patch)
	if errors.Is(err, gitsourcepkg.ErrGitSourceConflict) {
		writeError(w, http.StatusConflict, "git source name already exists")
		return
	}
	if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
		writeError(w, http.StatusNotFound, "git source not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if detail := gitSourceChangeDetail(existing, source); detail != "" {
		h.recordAudit(r, workspaceID, sourceID, "", "settings_changed", detail)
	}
	writeJSON(w, http.StatusOK, newCanonicalGitSourceView(source))
}

func applyGitSourcePatch(source gitsourcepkg.Source, patch gitsourcepkg.Patch) gitsourcepkg.Source {
	if patch.Name != nil {
		source.Name = *patch.Name
	}
	if patch.ID != nil {
		source.ID = *patch.ID
	}
	if patch.RepoURL != nil {
		source.RepoURL = *patch.RepoURL
	}
	if patch.Branch != nil {
		source.Branch = *patch.Branch
	}
	if patch.Subpath != nil {
		source.Subpath = *patch.Subpath
	}
	if patch.TokenEnv != nil {
		source.TokenEnv = *patch.TokenEnv
	}
	return source
}

func gitSourcePatchRequiresAccessCheck(before gitsourcepkg.Source, after gitsourcepkg.Source) bool {
	return before.RepoURL != after.RepoURL ||
		before.Branch != after.Branch ||
		before.TokenEnv != after.TokenEnv
}

func (h *Handler) handleCanonicalDeleteGitSource(w http.ResponseWriter, r *http.Request, workspaceID string, sourceID string) {
	var ok bool
	sourceID, ok = requireCanonicalGitSourceRouteID(w, sourceID)
	if !ok {
		return
	}
	deleter, ok := h.gitSources.(interface {
		Delete(context.Context, string, string) (bool, error)
	})
	if !ok {
		writeError(w, http.StatusNotImplemented, "git source delete is not supported")
		return
	}
	existing, getErr := h.gitSources.Get(r.Context(), workspaceID, sourceID)
	deleted, err := deleter.Delete(r.Context(), workspaceID, sourceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "git source not found")
		return
	}
	detail := ""
	if getErr == nil {
		detail = gitSourceAuditDetail(existing)
	}
	h.recordAudit(r, workspaceID, sourceID, "", "source_deleted", detail)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCanonicalGitSourceSync(w http.ResponseWriter, r *http.Request, workspaceID string, sourceID string) {
	var ok bool
	sourceID, ok = requireCanonicalGitSourceRouteID(w, sourceID)
	if !ok {
		return
	}
	var request canonicalGitSourceSyncRequest
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	expectedCommit, ok := expectedGitCommit(request.ExpectedCommit, request.ExpectedCamel)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_commit is invalid")
		return
	}
	if h.syncer == nil {
		writeError(w, http.StatusServiceUnavailable, "sync API is not configured")
		return
	}
	if h.gitSources == nil {
		writeError(w, http.StatusServiceUnavailable, "git source registry is not configured")
		return
	}
	source, err := h.gitSources.Get(r.Context(), workspaceID, sourceID)
	if err != nil {
		if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
			writeError(w, http.StatusNotFound, "git source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	candidate, ok := h.syncGitSourceRevision(w, r, workspaceID, source, expectedCommit)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCanonicalSourceSyncResult(candidate))
}

func (h *Handler) handleCanonicalGitSourceDeploy(w http.ResponseWriter, r *http.Request, workspaceID string, sourceID string) {
	var ok bool
	sourceID, ok = requireCanonicalGitSourceRouteID(w, sourceID)
	if !ok {
		return
	}
	var request canonicalGitSourceDeployRequest
	if err := readOptionalJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !deployRequestConfirmed(request) {
		writeError(w, http.StatusBadRequest, "deploy confirmation is required")
		return
	}
	if firstNonEmpty(strings.TrimSpace(request.Commit), strings.TrimSpace(request.CommitCamel)) != "" {
		writeError(w, http.StatusBadRequest, "deploy always uses the latest synchronized revision; omit commit")
		return
	}
	expectedCommit, ok := expectedGitCommit(request.ExpectedCommit, request.ExpectedCamel)
	if !ok {
		writeError(w, http.StatusBadRequest, "expected_commit is invalid")
		return
	}
	actor := strings.TrimSpace(requestActorSubject(r))
	if actor == "" {
		writeError(w, http.StatusBadRequest, "deploy actor is required")
		return
	}
	if h.gitSources == nil {
		writeError(w, http.StatusServiceUnavailable, "git source registry is not configured")
		return
	}
	source, err := h.gitSources.Get(r.Context(), workspaceID, sourceID)
	if err != nil {
		if errors.Is(err, gitsourcepkg.ErrGitSourceNotFound) {
			writeError(w, http.StatusNotFound, "git source not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deploymentID := newDeploymentOperationID()
	message := deployRequestMessage(request)
	publication, ok := h.deployLatestGitSourceRevision(w, r, workspaceID, source, expectedCommit, gitSourceOperationAudit{
		Source:       "deploy",
		DeploymentID: &deploymentID,
		Message:      message,
		CreatedBy:    &actor,
	})
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCanonicalDeployResult(publication.Deployment, publication.ReleaseID))
}

func expectedGitCommit(values ...string) (string, bool) {
	value := strings.TrimSpace(firstNonEmpty(values...))
	if value == "" {
		return "", true
	}
	if len(value) > 128 {
		return "", false
	}
	for _, char := range value {
		if unicode.IsSpace(char) || unicode.IsControl(char) {
			return "", false
		}
	}
	return value, true
}

func deployRequestConfirmed(request canonicalGitSourceDeployRequest) bool {
	return request.Confirm || request.Confirmed || request.ConfirmCamel || request.ConfirmedCamel
}

func deployRequestMessage(request canonicalGitSourceDeployRequest) *string {
	value, ok := firstPresentString(request.Message, request.MessageCamel)
	if !ok {
		return nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func newDeploymentOperationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

func (h *Handler) requireGitSourceAccess(w http.ResponseWriter, r *http.Request, source gitsourcepkg.Source, token string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), probeTimeout)
	defer cancel()
	branch := strings.TrimSpace(source.Branch)
	if branch == "" {
		branch = "main"
	}
	branches, err := sourcepkg.ListRemoteBranches(ctx, strings.TrimSpace(source.RepoURL), token)
	if err != nil {
		writeGitSourceError(w, http.StatusUnprocessableEntity, gitSourceErrorRepositoryUnreachable, "repository cannot be reached with the provided Git credential")
		return false
	}
	if !stringSliceContains(branches, branch) {
		writeGitSourceError(w, http.StatusUnprocessableEntity, gitSourceErrorBranchNotFound, fmt.Sprintf("branch %q was not found in repository", branch))
		return false
	}
	return true
}

func writeGitSourceError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

func (h *Handler) resolveGitSourceCreds(ctx context.Context, workspaceID string, credsRef string) (string, error) {
	credsRef = strings.TrimSpace(credsRef)
	if credsRef == "" || h.store == nil {
		return "", nil
	}
	variable, found, err := h.store.GetVariable(ctx, workspaceID, "", credsRef)
	if err != nil || !found {
		return "", err
	}
	if variable.IsSecret {
		return h.decryptSecretVariable(ctx, workspaceID, variable.Path, variable.Value)
	}
	return variable.Value, nil
}

func gitCredentialFromRequest(request gitCredentialRequest) (string, error) {
	authMethod := strings.ToLower(strings.TrimSpace(request.AuthMethod))
	token := strings.TrimSpace(request.AccessToken)
	username := strings.TrimSpace(request.Username)
	password := strings.TrimSpace(request.Password)
	if authMethod == "" {
		authMethod = "pat"
		if token == "" && username == "" && password == "" {
			return "", nil
		}
		if username != "" || password != "" {
			authMethod = "basic"
		}
	}
	switch authMethod {
	case "none", "public":
		return "", nil
	case "pat", "token", "access_token":
		if token == "" {
			return "", errors.New("access_token is required for personal access token authentication")
		}
		return mustGitCredentialJSON(gitCredentialRequest{AuthMethod: "pat", AccessToken: token})
	case "basic", "password":
		if username == "" || password == "" {
			return "", errors.New("username and password are required for username/password authentication")
		}
		return mustGitCredentialJSON(gitCredentialRequest{AuthMethod: "basic", Username: username, Password: password})
	default:
		return "", fmt.Errorf("unsupported auth_method %q", request.AuthMethod)
	}
}

func gitSourceRegisterHasInlineCredential(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func mustGitCredentialJSON(request gitCredentialRequest) (string, error) {
	payload := map[string]string{"type": strings.ToLower(strings.TrimSpace(request.AuthMethod))}
	if payload["type"] == "basic" {
		payload["username"] = strings.TrimSpace(request.Username)
		payload["password"] = strings.TrimSpace(request.Password)
	} else {
		payload["token"] = strings.TrimSpace(request.AccessToken)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// manifestFileName reports the manifest name this instance reads at app source
// roots. The syncer owns the operator's choice; sample sources must write the
// same name or the instance cannot read what it just created.
func (h *Handler) manifestFileName() string {
	if h.syncer == nil {
		return ""
	}
	return h.syncer.ManifestFile
}
