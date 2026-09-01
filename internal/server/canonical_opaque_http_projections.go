package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	maxOpaqueHTTPProjectionBodyBytes = int64(1 << 20)
	maxOpaqueHTTPRequestBodyBytes    = int64(16 << 20)
	maxOpaqueIngressCredentialRefs   = 64
	maxOpaqueIngressReferences       = 31
)

var (
	opaqueProjectionOperationIDPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	opaqueProjectionOperationRefPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]*(?:/[a-z][a-z0-9.-]*)+$`)
	opaqueProjectionPublicationPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	opaqueProjectionNamedPinPattern     = regexp.MustCompile(`^[a-z][A-Za-z0-9.-]*$`)
	opaqueProjectionSHA256Pattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type opaqueIngressCredentialSnapshotInput struct {
	Issuer              string                                   `json:"issuer"`
	Audience            string                                   `json:"audience"`
	Reference           state.OpaqueIngressCredentialSnapshotRef `json:"reference"`
	OperationRef        string                                   `json:"operationRef"`
	References          []contract.NamedImmutableReferencePin    `json:"references,omitempty"`
	ProjectedAt         time.Time                                `json:"projectedAt"`
	NotAfter            time.Time                                `json:"notAfter"`
	MaxStalenessSeconds int64                                    `json:"maxStalenessSeconds"`
	OperationID         string                                   `json:"operationId"`
}

type opaqueIngressCredentialRevocationInput struct {
	Issuer      string                                   `json:"issuer"`
	Audience    string                                   `json:"audience"`
	Reference   state.OpaqueIngressCredentialSnapshotRef `json:"reference"`
	OperationID string                                   `json:"operationId"`
}

type opaqueIngressPublicationRevisionInput struct {
	Issuer              string                                     `json:"issuer"`
	Audience            string                                     `json:"audience"`
	PublicationRef      string                                     `json:"publicationRef"`
	Revision            string                                     `json:"revision"`
	Digest              string                                     `json:"digest"`
	App                 string                                     `json:"app"`
	Action              string                                     `json:"action"`
	Release             state.OpaqueIngressReleasePin              `json:"release"`
	HTTP                state.OpaqueIngressHTTPContract            `json:"http"`
	OperationRef        string                                     `json:"operationRef"`
	CredentialRefs      []state.OpaqueIngressCredentialSnapshotRef `json:"credentialRefs"`
	References          []contract.NamedImmutableReferencePin      `json:"references,omitempty"`
	ProjectedAt         time.Time                                  `json:"projectedAt"`
	NotAfter            time.Time                                  `json:"notAfter"`
	MaxStalenessSeconds int64                                      `json:"maxStalenessSeconds"`
	RetainUntil         time.Time                                  `json:"retainUntil"`
	OperationID         string                                     `json:"operationId"`
}

type opaqueIngressActivationInput struct {
	Issuer             string `json:"issuer"`
	Audience           string `json:"audience"`
	PublicationRef     string `json:"publicationRef"`
	ExpectedGeneration int64  `json:"expectedGeneration"`
	TargetRevision     string `json:"targetRevision"`
	Kind               string `json:"kind"`
	OperationID        string `json:"operationId"`
}

type opaqueIngressRetentionInput struct {
	Before      time.Time `json:"before"`
	Limit       int       `json:"limit"`
	OperationID string    `json:"operationId"`
}

type opaqueIngressCredentialSnapshotView struct {
	WorkspaceID         string                                   `json:"workspaceId"`
	Issuer              string                                   `json:"issuer"`
	Audience            string                                   `json:"audience"`
	Reference           state.OpaqueIngressCredentialSnapshotRef `json:"reference"`
	OperationRef        string                                   `json:"operationRef"`
	References          []contract.NamedImmutableReferencePin    `json:"references,omitempty"`
	ProjectedAt         time.Time                                `json:"projectedAt"`
	NotAfter            time.Time                                `json:"notAfter"`
	MaxStalenessSeconds int64                                    `json:"maxStalenessSeconds"`
	OperationID         string                                   `json:"operationId"`
	Actor               string                                   `json:"actor"`
	CreatedAt           time.Time                                `json:"createdAt"`
}

type opaqueIngressCredentialRevocationView struct {
	ID          string                                   `json:"id"`
	WorkspaceID string                                   `json:"workspaceId"`
	Issuer      string                                   `json:"issuer"`
	Audience    string                                   `json:"audience"`
	Reference   state.OpaqueIngressCredentialSnapshotRef `json:"reference"`
	OperationID string                                   `json:"operationId"`
	Actor       string                                   `json:"actor"`
	CreatedAt   time.Time                                `json:"createdAt"`
}

type opaqueIngressPublicationRevisionView struct {
	WorkspaceID         string                                     `json:"workspaceId"`
	Issuer              string                                     `json:"issuer"`
	Audience            string                                     `json:"audience"`
	PublicationRef      string                                     `json:"publicationRef"`
	Revision            string                                     `json:"revision"`
	Digest              string                                     `json:"digest"`
	App                 string                                     `json:"app"`
	Action              string                                     `json:"action"`
	Release             state.OpaqueIngressReleasePin              `json:"release"`
	HTTP                state.OpaqueIngressHTTPContract            `json:"http"`
	OperationRef        string                                     `json:"operationRef"`
	CredentialRefs      []state.OpaqueIngressCredentialSnapshotRef `json:"credentialRefs"`
	References          []contract.NamedImmutableReferencePin      `json:"references,omitempty"`
	ProjectedAt         time.Time                                  `json:"projectedAt"`
	NotAfter            time.Time                                  `json:"notAfter"`
	MaxStalenessSeconds int64                                      `json:"maxStalenessSeconds"`
	RetainUntil         time.Time                                  `json:"retainUntil"`
	OperationID         string                                     `json:"operationId"`
	Actor               string                                     `json:"actor"`
	CreatedAt           time.Time                                  `json:"createdAt"`
}

type opaqueIngressActivationView struct {
	WorkspaceID       string    `json:"workspaceId"`
	Issuer            string    `json:"issuer"`
	Audience          string    `json:"audience"`
	PublicationRef    string    `json:"publicationRef"`
	Generation        int64     `json:"generation"`
	Revision          string    `json:"revision"`
	PublicationDigest string    `json:"publicationDigest"`
	State             string    `json:"state"`
	Kind              string    `json:"kind"`
	OperationID       string    `json:"operationId"`
	Actor             string    `json:"actor"`
	CreatedAt         time.Time `json:"createdAt"`
}

type opaqueIngressAuditView struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspaceId"`
	Issuer         string    `json:"issuer"`
	Audience       string    `json:"audience"`
	PublicationRef string    `json:"publicationRef,omitempty"`
	Generation     int64     `json:"generation,omitempty"`
	SubjectKind    string    `json:"subjectKind"`
	SubjectID      string    `json:"subjectId"`
	Kind           string    `json:"kind"`
	OperationID    string    `json:"operationId"`
	Actor          string    `json:"actor"`
	CreatedAt      time.Time `json:"createdAt"`
}

type opaqueIngressRetentionView struct {
	PublicationRevisions int64 `json:"publicationRevisions"`
	CredentialSnapshots  int64 `json:"credentialSnapshots"`
}

func (h *Handler) handleCanonicalOpaqueHTTPProjectionAPI(w http.ResponseWriter, r *http.Request, parts []string) bool {
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "w" || parts[3] != "opaque-http-projections" {
		return false
	}
	if len(parts) == 5 {
		switch parts[4] {
		case "credential-snapshots":
			if !requireOpaqueProjectionMethod(w, r, http.MethodPost) {
				return true
			}
			h.handleOpaqueIngressCredentialSnapshot(w, r, parts[2])
			return true
		case "credential-revocations":
			if !requireOpaqueProjectionMethod(w, r, http.MethodPost) {
				return true
			}
			h.handleOpaqueIngressCredentialRevocation(w, r, parts[2])
			return true
		case "publication-revisions":
			if !requireOpaqueProjectionMethod(w, r, http.MethodPost) {
				return true
			}
			h.handleOpaqueIngressPublicationRevision(w, r, parts[2])
			return true
		case "activations":
			if !requireOpaqueProjectionMethod(w, r, http.MethodPost) {
				return true
			}
			h.handleOpaqueIngressActivation(w, r, parts[2])
			return true
		case "audit":
			if !requireOpaqueProjectionMethod(w, r, http.MethodGet) {
				return true
			}
			h.handleOpaqueIngressAudit(w, r, parts[2])
			return true
		}
	}
	if len(parts) == 6 && parts[4] == "retention" && parts[5] == "prune" {
		if !requireOpaqueProjectionMethod(w, r, http.MethodPost) {
			return true
		}
		h.handleOpaqueIngressRetention(w, r, parts[2])
		return true
	}
	writeOpaqueProjectionError(w, http.StatusNotFound, "projection endpoint not found")
	return true
}

func (h *Handler) handleOpaqueIngressCredentialSnapshot(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !opaqueProjectionAdmin(r, workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	var input opaqueIngressCredentialSnapshotInput
	if !readOpaqueProjectionInput(w, r, &input) {
		return
	}
	input.normalize()
	if !validateOpaqueCredentialSnapshotInput(input) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	request := state.OpaqueIngressCredentialSnapshotRequest{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID), Issuer: input.Issuer, Audience: input.Audience,
		Reference: input.Reference, OperationRef: input.OperationRef, References: input.References,
		ProjectedAt: input.ProjectedAt, NotAfter: input.NotAfter, MaxStalenessSeconds: input.MaxStalenessSeconds,
		OperationID: input.OperationID, Actor: opaqueProjectionActor(r),
	}
	request.RequestFingerprint = opaqueProjectionFingerprint("credential-snapshot", input)
	result, replay, err := h.store.PutOpaqueIngressCredentialSnapshot(r.Context(), request)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	writeJSON(w, opaqueProjectionMutationStatus(replay), opaqueIngressCredentialSnapshotResponse(result))
}

func (h *Handler) handleOpaqueIngressCredentialRevocation(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !opaqueProjectionAdmin(r, workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	var input opaqueIngressCredentialRevocationInput
	if !readOpaqueProjectionInput(w, r, &input) {
		return
	}
	input.normalize()
	if !validateOpaqueCredentialRevocationInput(input) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	request := state.OpaqueIngressCredentialRevocationRequest{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID), Issuer: input.Issuer, Audience: input.Audience,
		Reference: input.Reference, OperationID: input.OperationID, Actor: opaqueProjectionActor(r),
	}
	request.RequestFingerprint = opaqueProjectionFingerprint("credential-revocation", input)
	result, replay, err := h.store.RevokeOpaqueIngressCredentialSnapshot(r.Context(), request)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	writeJSON(w, opaqueProjectionMutationStatus(replay), opaqueIngressCredentialRevocationResponse(result))
}

func (h *Handler) handleOpaqueIngressPublicationRevision(w http.ResponseWriter, r *http.Request, workspaceID string) {
	principal := workspacePrincipalFrom(r.Context())
	if !opaqueProjectionAdminOrService(principal, workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	var input opaqueIngressPublicationRevisionInput
	if !readOpaqueProjectionInput(w, r, &input) {
		return
	}
	input.normalize()
	if !validateOpaquePublicationRevisionInput(input) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	if principal != nil && !principal.Admin {
		target, ok := opaqueProjectionExactServiceTarget(principal)
		if !ok || target != input.App+"/"+input.Action {
			writeOpaqueProjectionError(w, http.StatusForbidden, "projection target forbidden")
			return
		}
	}
	revision := state.OpaqueIngressPublicationRevision{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID), Issuer: input.Issuer, Audience: input.Audience,
		PublicationRef: input.PublicationRef, Revision: input.Revision, Digest: input.Digest,
		App: input.App, Action: input.Action, Release: input.Release, HTTP: input.HTTP,
		OperationRef: input.OperationRef, CredentialRefs: input.CredentialRefs, References: input.References,
		ProjectedAt: input.ProjectedAt, NotAfter: input.NotAfter, MaxStalenessSeconds: input.MaxStalenessSeconds,
		RetainUntil: input.RetainUntil,
	}
	request := state.OpaqueIngressPublicationRevisionRequest{
		Revision: revision, OperationID: input.OperationID, Actor: opaqueProjectionActor(r),
	}
	request.RequestFingerprint = opaqueProjectionFingerprint("publication-revision", input)
	result, replay, err := h.store.PutOpaqueIngressPublicationRevision(r.Context(), request)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	writeJSON(w, opaqueProjectionMutationStatus(replay), opaqueIngressPublicationRevisionResponse(result))
}

func (h *Handler) handleOpaqueIngressActivation(w http.ResponseWriter, r *http.Request, workspaceID string) {
	principal := workspacePrincipalFrom(r.Context())
	if !opaqueProjectionAdminOrService(principal, workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	var input opaqueIngressActivationInput
	if !readOpaqueProjectionInput(w, r, &input) {
		return
	}
	input.normalize()
	if !validateOpaqueActivationInput(input) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	authorizedTarget := ""
	if principal != nil && !principal.Admin {
		var ok bool
		authorizedTarget, ok = opaqueProjectionExactServiceTarget(principal)
		if !ok {
			writeOpaqueProjectionError(w, http.StatusForbidden, "projection target forbidden")
			return
		}
	}
	request := state.OpaqueIngressActivationRequest{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID), Issuer: input.Issuer, Audience: input.Audience,
		PublicationRef: input.PublicationRef, ExpectedGeneration: input.ExpectedGeneration,
		TargetRevision: input.TargetRevision, Kind: input.Kind, AuthorizedTarget: authorizedTarget,
		OperationID: input.OperationID, Actor: opaqueProjectionActor(r),
	}
	request.RequestFingerprint = opaqueProjectionFingerprint("activation", input)
	result, replay, err := h.store.ActivateOpaqueIngressPublication(r.Context(), request)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	writeJSON(w, opaqueProjectionMutationStatus(replay), opaqueIngressActivationResponse(result))
}

func (h *Handler) handleOpaqueIngressAudit(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !opaqueProjectionAdminOrService(workspacePrincipalFrom(r.Context()), workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || !validOpaqueAuditQuery(query) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	publicationRef := strings.TrimSpace(query.Get("publicationRef"))
	if publicationRef != "" && !validOpaquePublicationRef(publicationRef) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	limit := 100
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
			return
		}
		limit = parsed
	}
	result, err := h.store.ListOpaqueIngressProjectionAudit(r.Context(), contract.NormalizeWorkspace(workspaceID), publicationRef, limit)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	views := make([]opaqueIngressAuditView, 0, len(result))
	for _, record := range result {
		views = append(views, opaqueIngressAuditResponse(record))
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *Handler) handleOpaqueIngressRetention(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !opaqueProjectionAdmin(r, workspaceID) {
		writeOpaqueProjectionError(w, http.StatusForbidden, "projection operation forbidden")
		return
	}
	var input opaqueIngressRetentionInput
	if !readOpaqueProjectionInput(w, r, &input) {
		return
	}
	input.normalize()
	if input.Before.IsZero() || input.Limit < 1 || input.Limit > 1000 || !validOpaqueOperationID(input.OperationID) {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return
	}
	request := state.OpaqueIngressRetentionRequest{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID), Before: input.Before, Limit: input.Limit,
		OperationID: input.OperationID, Actor: opaqueProjectionActor(r),
	}
	request.RequestFingerprint = opaqueProjectionFingerprint("retention-prune", input)
	result, replay, err := h.store.PruneOpaqueIngressProjectionHistory(r.Context(), request)
	if err != nil {
		writeOpaqueProjectionStateError(w, err)
		return
	}
	writeJSON(w, opaqueProjectionMutationStatus(replay), opaqueIngressRetentionResponse(result))
}

func readOpaqueProjectionInput(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeOpaqueProjectionError(w, http.StatusUnsupportedMediaType, "application/json required")
		return false
	}
	if err := readStrictJSONBody(r.Body, maxOpaqueHTTPProjectionBodyBytes, target); err != nil {
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
		return false
	}
	return true
}

func requireOpaqueProjectionMethod(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	w.Header().Set("Allow", allowed)
	writeOpaqueProjectionError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func opaqueProjectionAdmin(r *http.Request, workspaceID string) bool {
	principal := workspacePrincipalFrom(r.Context())
	return principal != nil && principal.Admin && opaqueProjectionWorkspaceMatches(principal, workspaceID)
}

func opaqueProjectionAdminOrService(principal *workspacePrincipal, workspaceID string) bool {
	return principal != nil && opaqueProjectionWorkspaceMatches(principal, workspaceID) && (principal.Admin || principal.Service != nil)
}

func opaqueProjectionWorkspaceMatches(principal *workspacePrincipal, workspaceID string) bool {
	want := contract.NormalizeWorkspace(workspaceID)
	got := contract.NormalizeWorkspace(principal.Workspace)
	return got == want
}

func opaqueProjectionActor(r *http.Request) string {
	principal := workspacePrincipalFrom(r.Context())
	if principal == nil || principal.Admin {
		return "admin"
	}
	return strings.TrimSpace(principal.Subject)
}

func opaqueProjectionExactServiceTarget(principal *workspacePrincipal) (string, bool) {
	if principal == nil || principal.Service == nil || len(principal.Service.AllowedTargets) != 1 {
		return "", false
	}
	target := strings.TrimSpace(principal.Service.AllowedTargets[0])
	if strings.Count(target, "/") != 1 {
		return "", false
	}
	app, action, _ := strings.Cut(target, "/")
	if !contract.ValidAppKey(app) || !contract.ValidActionKey(action) {
		return "", false
	}
	return app + "/" + action, true
}

func opaqueProjectionMutationStatus(replay bool) int {
	if replay {
		return http.StatusOK
	}
	return http.StatusCreated
}

func opaqueProjectionFingerprint(kind string, input any) string {
	raw, _ := json.Marshal(struct {
		Kind  string `json:"kind"`
		Input any    `json:"input"`
	}{Kind: kind, Input: input})
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeOpaqueProjectionStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, state.ErrNotFound):
		writeOpaqueProjectionError(w, http.StatusNotFound, "projection not found")
	case errors.Is(err, state.ErrConflict):
		writeOpaqueProjectionError(w, http.StatusConflict, "projection conflict")
	case errors.Is(err, state.ErrInvalidState):
		writeOpaqueProjectionError(w, http.StatusBadRequest, "malformed request")
	case errors.Is(err, state.ErrOpaqueIngressProjectionRejected), errors.Is(err, state.ErrForbidden):
		writeOpaqueProjectionError(w, http.StatusUnprocessableEntity, "projection rejected")
	default:
		writeOpaqueProjectionError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeOpaqueProjectionError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (input *opaqueIngressCredentialSnapshotInput) normalize() {
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.Audience = strings.TrimSpace(input.Audience)
	input.Reference = normalizeOpaqueCredentialRef(input.Reference)
	input.OperationRef = strings.TrimSpace(input.OperationRef)
	input.References = normalizeOpaqueNamedReferences(input.References)
	input.ProjectedAt = input.ProjectedAt.UTC()
	input.NotAfter = input.NotAfter.UTC()
	input.OperationID = strings.TrimSpace(input.OperationID)
}

func (input *opaqueIngressCredentialRevocationInput) normalize() {
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.Audience = strings.TrimSpace(input.Audience)
	input.Reference = normalizeOpaqueCredentialRef(input.Reference)
	input.OperationID = strings.TrimSpace(input.OperationID)
}

func (input *opaqueIngressPublicationRevisionInput) normalize() {
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.Audience = strings.TrimSpace(input.Audience)
	input.PublicationRef = strings.TrimSpace(input.PublicationRef)
	input.Revision = strings.TrimSpace(input.Revision)
	input.Digest = strings.TrimSpace(input.Digest)
	input.App = strings.TrimSpace(input.App)
	input.Action = strings.TrimSpace(input.Action)
	input.Release.DeploymentID = strings.TrimSpace(input.Release.DeploymentID)
	input.Release.Commit = strings.TrimSpace(input.Release.Commit)
	input.Release.BundleDigest = strings.TrimSpace(input.Release.BundleDigest)
	input.HTTP.Method = strings.ToUpper(strings.TrimSpace(input.HTTP.Method))
	input.HTTP.ExactEscapedPath = strings.TrimSpace(input.HTTP.ExactEscapedPath)
	input.HTTP.ContentType = strings.TrimSpace(input.HTTP.ContentType)
	input.HTTP.ResponsePolicy.ContentTypes = append([]string(nil), input.HTTP.ResponsePolicy.ContentTypes...)
	for index := range input.HTTP.ResponsePolicy.ContentTypes {
		input.HTTP.ResponsePolicy.ContentTypes[index] = strings.TrimSpace(input.HTTP.ResponsePolicy.ContentTypes[index])
	}
	sort.Strings(input.HTTP.ResponsePolicy.ContentTypes)
	input.OperationRef = strings.TrimSpace(input.OperationRef)
	for index := range input.CredentialRefs {
		input.CredentialRefs[index] = normalizeOpaqueCredentialRef(input.CredentialRefs[index])
	}
	sort.Slice(input.CredentialRefs, func(i, j int) bool {
		if input.CredentialRefs[i].ID == input.CredentialRefs[j].ID {
			return input.CredentialRefs[i].Revision < input.CredentialRefs[j].Revision
		}
		return input.CredentialRefs[i].ID < input.CredentialRefs[j].ID
	})
	input.References = normalizeOpaqueNamedReferences(input.References)
	input.ProjectedAt = input.ProjectedAt.UTC()
	input.NotAfter = input.NotAfter.UTC()
	input.RetainUntil = input.RetainUntil.UTC()
	input.OperationID = strings.TrimSpace(input.OperationID)
}

func (input *opaqueIngressActivationInput) normalize() {
	input.Issuer = strings.TrimSpace(input.Issuer)
	input.Audience = strings.TrimSpace(input.Audience)
	input.PublicationRef = strings.TrimSpace(input.PublicationRef)
	input.TargetRevision = strings.TrimSpace(input.TargetRevision)
	input.Kind = strings.TrimSpace(input.Kind)
	input.OperationID = strings.TrimSpace(input.OperationID)
}

func (input *opaqueIngressRetentionInput) normalize() {
	input.Before = input.Before.UTC()
	input.OperationID = strings.TrimSpace(input.OperationID)
}

func normalizeOpaqueCredentialRef(reference state.OpaqueIngressCredentialSnapshotRef) state.OpaqueIngressCredentialSnapshotRef {
	reference.ID = strings.TrimSpace(reference.ID)
	reference.Revision = strings.TrimSpace(reference.Revision)
	reference.Digest = strings.TrimSpace(reference.Digest)
	return reference
}

func normalizeOpaqueNamedReferences(references []contract.NamedImmutableReferencePin) []contract.NamedImmutableReferencePin {
	result := append([]contract.NamedImmutableReferencePin(nil), references...)
	for index := range result {
		result[index].Name = strings.TrimSpace(result[index].Name)
		result[index].Reference.ID = strings.TrimSpace(result[index].Reference.ID)
		result[index].Reference.Version = strings.TrimSpace(result[index].Reference.Version)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func validateOpaqueCredentialSnapshotInput(input opaqueIngressCredentialSnapshotInput) bool {
	return validOpaqueIssuerAudience(input.Issuer, input.Audience) &&
		validOpaqueCredentialRef(input.Reference) &&
		len(input.OperationRef) <= 200 && opaqueProjectionOperationRefPattern.MatchString(input.OperationRef) &&
		validOpaqueNamedReferences(input.References) &&
		validOpaqueFreshness(input.ProjectedAt, input.NotAfter, input.MaxStalenessSeconds) &&
		validOpaqueOperationID(input.OperationID)
}

func validateOpaqueCredentialRevocationInput(input opaqueIngressCredentialRevocationInput) bool {
	return validOpaqueIssuerAudience(input.Issuer, input.Audience) &&
		validOpaqueCredentialRef(input.Reference) &&
		validOpaqueOperationID(input.OperationID)
}

func validateOpaquePublicationRevisionInput(input opaqueIngressPublicationRevisionInput) bool {
	if !validOpaqueIssuerAudience(input.Issuer, input.Audience) || !validOpaquePublicationRef(input.PublicationRef) ||
		input.Revision == "" || len(input.Revision) > 200 || !opaqueProjectionSHA256Pattern.MatchString(input.Digest) ||
		!contract.ValidAppKey(input.App) || !contract.ValidActionKey(input.Action) ||
		!validBoundedOpaque(input.Release.DeploymentID, 200) || !validBoundedOpaque(input.Release.Commit, 200) || !opaqueProjectionSHA256Pattern.MatchString(input.Release.BundleDigest) ||
		len(input.OperationRef) > 200 || !opaqueProjectionOperationRefPattern.MatchString(input.OperationRef) ||
		len(input.CredentialRefs) == 0 || len(input.CredentialRefs) > maxOpaqueIngressCredentialRefs ||
		!validOpaqueNamedReferences(input.References) || !validOpaqueFreshness(input.ProjectedAt, input.NotAfter, input.MaxStalenessSeconds) ||
		input.RetainUntil.IsZero() || input.RetainUntil.Before(input.NotAfter) || !validOpaqueOperationID(input.OperationID) {
		return false
	}
	if input.HTTP.Method == "" || len(input.HTTP.Method) > 32 || !validBoundedOpaque(input.HTTP.ExactEscapedPath, 1024) || !strings.HasPrefix(input.HTTP.ExactEscapedPath, "/") ||
		input.HTTP.ContentType == "" || len(input.HTTP.ContentType) > 160 || input.HTTP.MaxRequestBodyBytes <= 0 || input.HTTP.MaxRequestBodyBytes > maxOpaqueHTTPRequestBodyBytes ||
		input.HTTP.ResponsePolicy.MaxBodyBytes <= 0 || input.HTTP.ResponsePolicy.MaxBodyBytes > contract.MaxApplicationWireResponseBodyBytes || len(input.HTTP.ResponsePolicy.ContentTypes) == 0 || len(input.HTTP.ResponsePolicy.ContentTypes) > 16 {
		return false
	}
	seenCredentials := make(map[string]struct{}, len(input.CredentialRefs))
	for _, reference := range input.CredentialRefs {
		if !validOpaqueCredentialRef(reference) {
			return false
		}
		key := reference.ID + "\x00" + reference.Revision
		if _, duplicate := seenCredentials[key]; duplicate {
			return false
		}
		seenCredentials[key] = struct{}{}
	}
	for _, contentType := range input.HTTP.ResponsePolicy.ContentTypes {
		if !validBoundedOpaque(strings.TrimSpace(contentType), 160) {
			return false
		}
	}
	return true
}

func validateOpaqueActivationInput(input opaqueIngressActivationInput) bool {
	if !validOpaqueIssuerAudience(input.Issuer, input.Audience) || !validOpaquePublicationRef(input.PublicationRef) || input.ExpectedGeneration < 0 || !validOpaqueOperationID(input.OperationID) {
		return false
	}
	switch input.Kind {
	case state.OpaqueIngressActivationKindActivate, state.OpaqueIngressActivationKindRollback:
		return validBoundedOpaque(input.TargetRevision, 200)
	case state.OpaqueIngressActivationKindRevoke:
		return input.TargetRevision == ""
	default:
		return false
	}
}

func validOpaqueIssuerAudience(issuer, audience string) bool {
	return validBoundedOpaque(issuer, 160) && validBoundedOpaque(audience, 160)
}

func validOpaquePublicationRef(value string) bool {
	return len(value) <= 100 && opaqueProjectionPublicationPattern.MatchString(value)
}

func validOpaqueOperationID(value string) bool {
	return opaqueProjectionOperationIDPattern.MatchString(value)
}

func validOpaqueCredentialRef(reference state.OpaqueIngressCredentialSnapshotRef) bool {
	return validBoundedOpaque(reference.ID, 200) && opaqueProjectionSHA256Pattern.MatchString(reference.Revision) && opaqueProjectionSHA256Pattern.MatchString(reference.Digest)
}

func validOpaqueNamedReferences(references []contract.NamedImmutableReferencePin) bool {
	if len(references) > maxOpaqueIngressReferences {
		return false
	}
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if len(reference.Name) > 64 || !opaqueProjectionNamedPinPattern.MatchString(reference.Name) || !validBoundedOpaque(reference.Reference.ID, 200) || !validBoundedOpaque(reference.Reference.Version, 200) {
			return false
		}
		if _, duplicate := seen[reference.Name]; duplicate {
			return false
		}
		seen[reference.Name] = struct{}{}
	}
	return true
}

func validOpaqueFreshness(projectedAt, notAfter time.Time, maxStalenessSeconds int64) bool {
	return !projectedAt.IsZero() && !notAfter.IsZero() && notAfter.After(projectedAt) && maxStalenessSeconds > 0 && maxStalenessSeconds <= 2_592_000
}

func validOpaqueAuditQuery(query url.Values) bool {
	for name, values := range query {
		if name != "publicationRef" && name != "limit" || len(values) != 1 {
			return false
		}
	}
	return true
}

func validBoundedOpaque(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func opaqueIngressCredentialSnapshotResponse(value state.OpaqueIngressCredentialSnapshot) opaqueIngressCredentialSnapshotView {
	return opaqueIngressCredentialSnapshotView{
		WorkspaceID: value.WorkspaceID, Issuer: value.Issuer, Audience: value.Audience,
		Reference: value.Reference, OperationRef: value.OperationRef, References: value.References,
		ProjectedAt: value.ProjectedAt, NotAfter: value.NotAfter, MaxStalenessSeconds: value.MaxStalenessSeconds,
		OperationID: value.OperationID, Actor: value.Actor, CreatedAt: value.CreatedAt,
	}
}

func opaqueIngressCredentialRevocationResponse(value state.OpaqueIngressCredentialRevocation) opaqueIngressCredentialRevocationView {
	return opaqueIngressCredentialRevocationView{
		ID: value.ID, WorkspaceID: value.WorkspaceID, Issuer: value.Issuer, Audience: value.Audience,
		Reference: value.Reference, OperationID: value.OperationID, Actor: value.Actor, CreatedAt: value.CreatedAt,
	}
}

func opaqueIngressPublicationRevisionResponse(value state.OpaqueIngressPublicationRevision) opaqueIngressPublicationRevisionView {
	return opaqueIngressPublicationRevisionView{
		WorkspaceID: value.WorkspaceID, Issuer: value.Issuer, Audience: value.Audience,
		PublicationRef: value.PublicationRef, Revision: value.Revision, Digest: value.Digest,
		App: value.App, Action: value.Action, Release: value.Release, HTTP: value.HTTP,
		OperationRef: value.OperationRef, CredentialRefs: value.CredentialRefs, References: value.References,
		ProjectedAt: value.ProjectedAt, NotAfter: value.NotAfter, MaxStalenessSeconds: value.MaxStalenessSeconds,
		RetainUntil: value.RetainUntil, OperationID: value.OperationID, Actor: value.Actor, CreatedAt: value.CreatedAt,
	}
}

func opaqueIngressActivationResponse(value state.OpaqueIngressActivation) opaqueIngressActivationView {
	return opaqueIngressActivationView{
		WorkspaceID: value.WorkspaceID, Issuer: value.Issuer, Audience: value.Audience,
		PublicationRef: value.PublicationRef, Generation: value.Generation, Revision: value.Revision,
		PublicationDigest: value.PublicationDigest, State: value.State, Kind: value.Kind,
		OperationID: value.OperationID, Actor: value.Actor, CreatedAt: value.CreatedAt,
	}
}

func opaqueIngressAuditResponse(value state.OpaqueIngressAudit) opaqueIngressAuditView {
	return opaqueIngressAuditView{
		ID: value.ID, WorkspaceID: value.WorkspaceID, Issuer: value.Issuer, Audience: value.Audience,
		PublicationRef: value.PublicationRef, Generation: value.Generation, SubjectKind: value.SubjectKind,
		SubjectID: value.SubjectID, Kind: value.Kind, OperationID: value.OperationID,
		Actor: value.Actor, CreatedAt: value.CreatedAt,
	}
}

func opaqueIngressRetentionResponse(value state.OpaqueIngressRetentionResult) opaqueIngressRetentionView {
	return opaqueIngressRetentionView{
		PublicationRevisions: value.PublicationRevisions,
		CredentialSnapshots:  value.CredentialSnapshots,
	}
}
