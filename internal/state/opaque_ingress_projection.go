package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

var ErrOpaqueIngressProjectionRejected = errors.New("opaque ingress projection rejected")

const (
	OpaqueIngressActivationActive  = "active"
	OpaqueIngressActivationRevoked = "revoked"

	OpaqueIngressActivationKindActivate = "activate"
	OpaqueIngressActivationKindRollback = "rollback"
	OpaqueIngressActivationKindRevoke   = "revoke"
)

// OpaqueIngressReleasePin identifies the exact active Release which a
// publication revision is allowed to invoke.
type OpaqueIngressReleasePin struct {
	DeploymentID string `json:"deploymentId"`
	Commit       string `json:"commit"`
	BundleDigest string `json:"bundleDigest"`
}

// OpaqueIngressCredentialSnapshotRef is a non-secret immutable reference.
// Digest binds the reference identity and prevents a mixed snapshot from being
// substituted under the same ID and revision.
type OpaqueIngressCredentialSnapshotRef struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Digest   string `json:"digest"`
}

type OpaqueIngressCredentialSnapshot struct {
	WorkspaceID         string                                `json:"workspaceId"`
	Issuer              string                                `json:"issuer"`
	Audience            string                                `json:"audience"`
	Reference           OpaqueIngressCredentialSnapshotRef    `json:"reference"`
	OperationRef        string                                `json:"operationRef"`
	References          []contract.NamedImmutableReferencePin `json:"references,omitempty"`
	ProjectedAt         time.Time                             `json:"projectedAt"`
	NotAfter            time.Time                             `json:"notAfter"`
	MaxStalenessSeconds int64                                 `json:"maxStalenessSeconds"`
	OperationID         string                                `json:"operationId"`
	RequestFingerprint  string                                `json:"requestFingerprint"`
	Actor               string                                `json:"actor"`
	CreatedAt           time.Time                             `json:"createdAt"`
}

type OpaqueIngressCredentialSnapshotRequest struct {
	WorkspaceID         string
	Issuer              string
	Audience            string
	Reference           OpaqueIngressCredentialSnapshotRef
	OperationRef        string
	References          []contract.NamedImmutableReferencePin
	ProjectedAt         time.Time
	NotAfter            time.Time
	MaxStalenessSeconds int64
	OperationID         string
	RequestFingerprint  string
	Actor               string
}

type OpaqueIngressCredentialRevocation struct {
	ID                 string                             `json:"id"`
	WorkspaceID        string                             `json:"workspaceId"`
	Issuer             string                             `json:"issuer"`
	Audience           string                             `json:"audience"`
	Reference          OpaqueIngressCredentialSnapshotRef `json:"reference"`
	Reason             string                             `json:"reason,omitempty"`
	OperationID        string                             `json:"operationId"`
	RequestFingerprint string                             `json:"requestFingerprint"`
	Actor              string                             `json:"actor"`
	CreatedAt          time.Time                          `json:"createdAt"`
}

type OpaqueIngressCredentialRevocationRequest struct {
	WorkspaceID        string
	Issuer             string
	Audience           string
	Reference          OpaqueIngressCredentialSnapshotRef
	Reason             string
	OperationID        string
	RequestFingerprint string
	Actor              string
}

type OpaqueIngressHTTPContract struct {
	Method              string              `json:"method"`
	ExactEscapedPath    string              `json:"exactEscapedPath"`
	ContentType         string              `json:"contentType"`
	MaxRequestBodyBytes int64               `json:"maxRequestBodyBytes"`
	ResponsePolicy      contract.HTTPPolicy `json:"responsePolicy"`
}

// OpaqueIngressPublicationRevision is immutable. CredentialRefs is an exact,
// sorted set and may contain multiple customer credential snapshots.
type OpaqueIngressPublicationRevision struct {
	WorkspaceID         string                                `json:"workspaceId"`
	Issuer              string                                `json:"issuer"`
	Audience            string                                `json:"audience"`
	PublicationRef      string                                `json:"publicationRef"`
	Revision            string                                `json:"revision"`
	Digest              string                                `json:"digest"`
	App                 string                                `json:"app"`
	Action              string                                `json:"action"`
	Release             OpaqueIngressReleasePin               `json:"release"`
	HTTP                OpaqueIngressHTTPContract             `json:"http"`
	OperationRef        string                                `json:"operationRef"`
	CredentialRefs      []OpaqueIngressCredentialSnapshotRef  `json:"credentialRefs"`
	References          []contract.NamedImmutableReferencePin `json:"references,omitempty"`
	ProjectedAt         time.Time                             `json:"projectedAt"`
	NotAfter            time.Time                             `json:"notAfter"`
	MaxStalenessSeconds int64                                 `json:"maxStalenessSeconds"`
	RetainUntil         time.Time                             `json:"retainUntil"`
	OperationID         string                                `json:"operationId"`
	RequestFingerprint  string                                `json:"requestFingerprint"`
	Actor               string                                `json:"actor"`
	CreatedAt           time.Time                             `json:"createdAt"`
}

type OpaqueIngressPublicationRevisionRequest struct {
	Revision           OpaqueIngressPublicationRevision
	OperationID        string
	RequestFingerprint string
	Actor              string
}

type OpaqueIngressActivation struct {
	WorkspaceID        string    `json:"workspaceId"`
	Issuer             string    `json:"issuer"`
	Audience           string    `json:"audience"`
	PublicationRef     string    `json:"publicationRef"`
	Generation         int64     `json:"generation"`
	Revision           string    `json:"revision"`
	PublicationDigest  string    `json:"publicationDigest"`
	State              string    `json:"state"`
	Kind               string    `json:"kind"`
	AuthorizedTarget   string    `json:"authorizedTarget,omitempty"`
	OperationID        string    `json:"operationId"`
	RequestFingerprint string    `json:"requestFingerprint"`
	Actor              string    `json:"actor"`
	CreatedAt          time.Time `json:"createdAt"`
}

type OpaqueIngressProjectionHead struct {
	WorkspaceID       string    `json:"workspaceId"`
	Issuer            string    `json:"issuer"`
	Audience          string    `json:"audience"`
	PublicationRef    string    `json:"publicationRef"`
	Generation        int64     `json:"generation"`
	Revision          string    `json:"revision"`
	PublicationDigest string    `json:"publicationDigest"`
	State             string    `json:"state"`
	UpdatedBy         string    `json:"updatedBy"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type OpaqueIngressActivationRequest struct {
	WorkspaceID        string
	Issuer             string
	Audience           string
	PublicationRef     string
	ExpectedGeneration int64
	TargetRevision     string
	Kind               string
	AuthorizedTarget   string
	OperationID        string
	RequestFingerprint string
	Actor              string
}

type OpaqueIngressResolutionRequest struct {
	Issuer             string
	Audience           string
	PublicationRef     string
	RouteGeneration    int64
	CredentialID       string
	CredentialRevision string
	Method             string
	ExactEscapedPath   string
	ContentType        string
	BodyByteLength     int64
	Now                time.Time
}

type OpaqueIngressResolvedProjection struct {
	Publication OpaqueIngressPublicationRevision `json:"publication"`
	Activation  OpaqueIngressActivation          `json:"activation"`
	Credential  OpaqueIngressCredentialSnapshot  `json:"credential"`
}

type OpaqueIngressAudit struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspaceId"`
	Issuer         string    `json:"issuer"`
	Audience       string    `json:"audience"`
	PublicationRef string    `json:"publicationRef,omitempty"`
	Generation     int64     `json:"generation,omitempty"`
	SubjectKind    string    `json:"subjectKind"`
	SubjectID      string    `json:"subjectId"`
	Kind           string    `json:"kind"`
	Detail         string    `json:"detail,omitempty"`
	OperationID    string    `json:"operationId"`
	Actor          string    `json:"actor"`
	CreatedAt      time.Time `json:"createdAt"`
}

type OpaqueIngressOperation struct {
	WorkspaceID        string          `json:"workspaceId"`
	OperationID        string          `json:"operationId"`
	RequestFingerprint string          `json:"requestFingerprint"`
	Kind               string          `json:"kind"`
	Result             json.RawMessage `json:"result"`
	CreatedAt          time.Time       `json:"createdAt"`
}

type OpaqueIngressRetentionRequest struct {
	WorkspaceID        string
	Before             time.Time
	Limit              int
	OperationID        string
	RequestFingerprint string
	Actor              string
}

type OpaqueIngressRetentionResult struct {
	PublicationRevisions int64 `json:"publicationRevisions"`
	CredentialSnapshots  int64 `json:"credentialSnapshots"`
}

func normalizeOpaqueIngressCredentialRequest(request OpaqueIngressCredentialSnapshotRequest) (OpaqueIngressCredentialSnapshotRequest, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.Issuer = strings.TrimSpace(request.Issuer)
	request.Audience = strings.TrimSpace(request.Audience)
	request.Reference.ID = strings.TrimSpace(request.Reference.ID)
	request.Reference.Revision = strings.TrimSpace(request.Reference.Revision)
	request.Reference.Digest = strings.TrimSpace(request.Reference.Digest)
	request.OperationRef = strings.TrimSpace(request.OperationRef)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	var err error
	request.References, err = normalizeOpaqueIngressReferences(request.References)
	if err != nil {
		return request, err
	}
	if !validOpaqueIngressString(request.Issuer, 160) || !validOpaqueIngressString(request.Audience, 160) ||
		!validOpaqueIngressString(request.Reference.ID, 200) || !validOpaqueIngressString(request.OperationID, 200) ||
		!validOpaqueIngressString(request.RequestFingerprint, 256) || !validOpaqueIngressString(request.Actor, 256) ||
		len(request.OperationRef) > 200 || !opaqueIngressOperationRefPattern.MatchString(request.OperationRef) {
		return request, fmt.Errorf("%w: credential snapshot fields are required", ErrInvalidState)
	}
	if !opaqueIngressSHA256Pattern.MatchString(request.Reference.Revision) || !opaqueIngressSHA256Pattern.MatchString(request.Reference.Digest) {
		return request, fmt.Errorf("%w: credential revision and digest must be lowercase SHA-256 values", ErrInvalidState)
	}
	if err := validateOpaqueIngressFreshnessDefinition(request.ProjectedAt, request.NotAfter, request.MaxStalenessSeconds); err != nil {
		return request, err
	}
	candidate := OpaqueIngressCredentialSnapshot{
		WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
		Reference: request.Reference, OperationRef: request.OperationRef, References: request.References,
		ProjectedAt: request.ProjectedAt.UTC(), NotAfter: request.NotAfter.UTC(), MaxStalenessSeconds: request.MaxStalenessSeconds,
	}
	expected := OpaqueIngressCredentialSnapshotDigest(candidate)
	if request.Reference.Digest != expected {
		return request, fmt.Errorf("%w: credential snapshot digest mismatch", ErrOpaqueIngressProjectionRejected)
	}
	return request, nil
}

func normalizeOpaqueIngressPublicationRequest(request OpaqueIngressPublicationRevisionRequest) (OpaqueIngressPublicationRevisionRequest, error) {
	revision := request.Revision
	revision.WorkspaceID = contract.NormalizeWorkspace(revision.WorkspaceID)
	revision.Issuer = strings.TrimSpace(revision.Issuer)
	revision.Audience = strings.TrimSpace(revision.Audience)
	revision.PublicationRef = strings.TrimSpace(revision.PublicationRef)
	revision.Revision = strings.TrimSpace(revision.Revision)
	revision.App = strings.TrimSpace(revision.App)
	revision.Action = strings.TrimSpace(revision.Action)
	revision.Release.DeploymentID = strings.TrimSpace(revision.Release.DeploymentID)
	revision.Release.Commit = strings.TrimSpace(revision.Release.Commit)
	revision.Release.BundleDigest = strings.TrimSpace(revision.Release.BundleDigest)
	revision.HTTP.Method = strings.ToUpper(strings.TrimSpace(revision.HTTP.Method))
	revision.HTTP.ExactEscapedPath = strings.TrimSpace(revision.HTTP.ExactEscapedPath)
	revision.HTTP.ContentType = strings.TrimSpace(revision.HTTP.ContentType)
	revision.OperationRef = strings.TrimSpace(revision.OperationRef)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	var err error
	revision.References, err = normalizeOpaqueIngressReferences(revision.References)
	if err != nil {
		return request, err
	}
	if !validOpaqueIngressString(revision.Issuer, 160) || !validOpaqueIngressString(revision.Audience, 160) ||
		len(revision.PublicationRef) > 100 || !opaqueIngressPublicationRefPattern.MatchString(revision.PublicationRef) ||
		!validOpaqueIngressString(revision.Revision, 200) || !contract.ValidAppKey(revision.App) || !contract.ValidActionKey(revision.Action) ||
		!validOpaqueIngressString(revision.Release.DeploymentID, 200) || !validOpaqueIngressString(revision.Release.Commit, 200) ||
		!opaqueIngressSHA256Pattern.MatchString(revision.Release.BundleDigest) || len(revision.OperationRef) > 200 ||
		!opaqueIngressOperationRefPattern.MatchString(revision.OperationRef) || !validOpaqueIngressString(request.OperationID, 200) ||
		!validOpaqueIngressString(request.RequestFingerprint, 256) || !validOpaqueIngressString(request.Actor, 256) {
		return request, fmt.Errorf("%w: publication revision fields are invalid", ErrInvalidState)
	}
	if !validOpaqueIngressHTTPMethod(revision.HTTP.Method) || !validOpaqueIngressPath(revision.HTTP.ExactEscapedPath) ||
		!validOpaqueIngressMediaType(revision.HTTP.ContentType) || revision.HTTP.MaxRequestBodyBytes <= 0 ||
		revision.HTTP.MaxRequestBodyBytes > 16<<20 || revision.HTTP.ResponsePolicy.MaxBodyBytes <= 0 ||
		revision.HTTP.ResponsePolicy.MaxBodyBytes > contract.MaxApplicationWireResponseBodyBytes ||
		len(revision.HTTP.ResponsePolicy.ContentTypes) == 0 || len(revision.HTTP.ResponsePolicy.ContentTypes) > 16 {
		return request, fmt.Errorf("%w: publication HTTP contract is invalid", ErrInvalidState)
	}
	revision.HTTP.ResponsePolicy.ContentTypes = append([]string(nil), revision.HTTP.ResponsePolicy.ContentTypes...)
	for index := range revision.HTTP.ResponsePolicy.ContentTypes {
		revision.HTTP.ResponsePolicy.ContentTypes[index] = strings.TrimSpace(revision.HTTP.ResponsePolicy.ContentTypes[index])
	}
	sort.Strings(revision.HTTP.ResponsePolicy.ContentTypes)
	seenResponseTypes := make(map[string]struct{}, len(revision.HTTP.ResponsePolicy.ContentTypes))
	for _, contentType := range revision.HTTP.ResponsePolicy.ContentTypes {
		if !validOpaqueIngressMediaType(contentType) {
			return request, fmt.Errorf("%w: publication response media type is invalid", ErrInvalidState)
		}
		if _, duplicate := seenResponseTypes[contentType]; duplicate {
			return request, fmt.Errorf("%w: duplicate publication response media type", ErrConflict)
		}
		seenResponseTypes[contentType] = struct{}{}
	}
	if err := validateOpaqueIngressFreshnessDefinition(revision.ProjectedAt, revision.NotAfter, revision.MaxStalenessSeconds); err != nil {
		return request, err
	}
	revision.ProjectedAt = revision.ProjectedAt.UTC()
	revision.NotAfter = revision.NotAfter.UTC()
	revision.RetainUntil = revision.RetainUntil.UTC()
	if revision.RetainUntil.IsZero() || revision.RetainUntil.Before(revision.NotAfter) {
		return request, fmt.Errorf("%w: retainUntil must be at or after notAfter", ErrInvalidState)
	}
	if _, err := http.NewRequest(revision.HTTP.Method, "http://opaque.invalid"+revision.HTTP.ExactEscapedPath, nil); err != nil {
		return request, fmt.Errorf("%w: publication HTTP contract is invalid", ErrInvalidState)
	}
	credentialRefs := append([]OpaqueIngressCredentialSnapshotRef(nil), revision.CredentialRefs...)
	if len(credentialRefs) > 64 {
		return request, fmt.Errorf("%w: publication credential references exceed 64", ErrInvalidState)
	}
	for index := range credentialRefs {
		credentialRefs[index].ID = strings.TrimSpace(credentialRefs[index].ID)
		credentialRefs[index].Revision = strings.TrimSpace(credentialRefs[index].Revision)
		credentialRefs[index].Digest = strings.TrimSpace(credentialRefs[index].Digest)
		if !validOpaqueIngressString(credentialRefs[index].ID, 200) || !opaqueIngressSHA256Pattern.MatchString(credentialRefs[index].Revision) || !opaqueIngressSHA256Pattern.MatchString(credentialRefs[index].Digest) {
			return request, fmt.Errorf("%w: credential reference is invalid", ErrInvalidState)
		}
	}
	sort.Slice(credentialRefs, func(i, j int) bool {
		if credentialRefs[i].ID == credentialRefs[j].ID {
			return credentialRefs[i].Revision < credentialRefs[j].Revision
		}
		return credentialRefs[i].ID < credentialRefs[j].ID
	})
	for index := 1; index < len(credentialRefs); index++ {
		if credentialRefs[index-1].ID == credentialRefs[index].ID && credentialRefs[index-1].Revision == credentialRefs[index].Revision {
			return request, fmt.Errorf("%w: duplicate credential reference", ErrConflict)
		}
	}
	if len(credentialRefs) == 0 {
		return request, fmt.Errorf("%w: publication requires at least one credential reference", ErrInvalidState)
	}
	revision.CredentialRefs = credentialRefs
	revision.OperationID = request.OperationID
	revision.RequestFingerprint = request.RequestFingerprint
	revision.Actor = request.Actor
	request.Revision = revision
	if revision.Digest != OpaqueIngressPublicationRevisionDigest(revision) {
		return request, fmt.Errorf("%w: publication revision digest mismatch", ErrOpaqueIngressProjectionRejected)
	}
	return request, nil
}

func opaqueIngressReleaseMatches(release OpaqueIngressReleasePin, deploymentID *string, commit, bundleDigest string) bool {
	return deploymentID != nil && strings.TrimSpace(*deploymentID) == release.DeploymentID && strings.TrimSpace(commit) == release.Commit && strings.TrimSpace(bundleDigest) == release.BundleDigest
}

func opaqueIngressCredentialRefEqual(left, right OpaqueIngressCredentialSnapshotRef) bool {
	return left.ID == right.ID && left.Revision == right.Revision && left.Digest == right.Digest
}

func cloneOpaqueIngressPublication(value OpaqueIngressPublicationRevision) OpaqueIngressPublicationRevision {
	value.CredentialRefs = append([]OpaqueIngressCredentialSnapshotRef(nil), value.CredentialRefs...)
	value.References = append([]contract.NamedImmutableReferencePin(nil), value.References...)
	value.HTTP.ResponsePolicy = contract.CloneHTTPPolicy(value.HTTP.ResponsePolicy)
	return value
}

func cloneOpaqueIngressCredential(value OpaqueIngressCredentialSnapshot) OpaqueIngressCredentialSnapshot {
	value.References = append([]contract.NamedImmutableReferencePin(nil), value.References...)
	return value
}

var opaqueIngressReferenceNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9.-]*$`)
var opaqueIngressSHA256Pattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var opaqueIngressOperationRefPattern = regexp.MustCompile(`^[a-z][a-z0-9.-]*(?:/[a-z][a-z0-9.-]*)+$`)
var opaqueIngressPublicationRefPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func normalizeOpaqueIngressReferences(references []contract.NamedImmutableReferencePin) ([]contract.NamedImmutableReferencePin, error) {
	result := append([]contract.NamedImmutableReferencePin(nil), references...)
	for index := range result {
		result[index].Name = strings.TrimSpace(result[index].Name)
		result[index].Reference.ID = strings.TrimSpace(result[index].Reference.ID)
		result[index].Reference.Version = strings.TrimSpace(result[index].Reference.Version)
		if len(result[index].Name) > 64 || !opaqueIngressReferenceNamePattern.MatchString(result[index].Name) ||
			!validOpaqueIngressString(result[index].Reference.ID, 200) || !validOpaqueIngressString(result[index].Reference.Version, 200) {
			return nil, fmt.Errorf("%w: immutable reference is invalid", ErrInvalidState)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	for index := 1; index < len(result); index++ {
		if result[index-1].Name == result[index].Name {
			return nil, fmt.Errorf("%w: duplicate immutable reference name", ErrConflict)
		}
	}
	if len(result) > 31 {
		return nil, fmt.Errorf("%w: too many immutable references", ErrInvalidState)
	}
	return result, nil
}

func validOpaqueIngressString(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}
	return true
}

func validOpaqueIngressHTTPMethod(method string) bool {
	if len(method) == 0 || len(method) > 32 {
		return false
	}
	for _, character := range method {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func validOpaqueIngressPath(path string) bool {
	return validOpaqueIngressString(path, 1024) && strings.HasPrefix(path, "/") && !strings.ContainsAny(path, "?#\r\n")
}

func validOpaqueIngressMediaType(contentType string) bool {
	if !validOpaqueIngressString(contentType, 160) {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.Contains(mediaType, "/") && !strings.Contains(mediaType, "*")
}

func validateOpaqueIngressCombinedReferences(publication, credential []contract.NamedImmutableReferencePin) error {
	if len(publication)+len(credential) > 31 {
		return fmt.Errorf("%w: combined immutable references exceed 31", ErrOpaqueIngressProjectionRejected)
	}
	names := make(map[string]struct{}, len(publication)+len(credential))
	for _, reference := range append(append([]contract.NamedImmutableReferencePin(nil), publication...), credential...) {
		if _, exists := names[reference.Name]; exists {
			return fmt.Errorf("%w: mixed immutable reference names", ErrOpaqueIngressProjectionRejected)
		}
		names[reference.Name] = struct{}{}
	}
	return nil
}

func validateOpaqueIngressFreshnessDefinition(projectedAt, notAfter time.Time, maxStalenessSeconds int64) error {
	if projectedAt.IsZero() || notAfter.IsZero() || !notAfter.After(projectedAt) || maxStalenessSeconds <= 0 || maxStalenessSeconds > int64((30*24*time.Hour)/time.Second) {
		return fmt.Errorf("%w: projection freshness definition is invalid", ErrInvalidState)
	}
	return nil
}

func validateOpaqueIngressFreshAt(projectedAt, notAfter time.Time, maxStalenessSeconds int64, now time.Time) error {
	now = now.UTC()
	if now.Before(projectedAt) || !now.Before(notAfter) || now.Sub(projectedAt) > time.Duration(maxStalenessSeconds)*time.Second {
		return fmt.Errorf("%w: projection is future, expired, or stale", ErrOpaqueIngressProjectionRejected)
	}
	return nil
}
