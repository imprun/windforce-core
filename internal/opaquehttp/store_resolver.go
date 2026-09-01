package opaquehttp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const opaqueHTTPPublicationPinName = "opaque-http-publication"

// ProjectionStore is the atomic provider-neutral projection lookup required by
// StoreResolver. Implementations must validate the active publication,
// credential snapshot, freshness, and exact active Release in one store
// operation.
type ProjectionStore interface {
	ResolveOpaqueIngressProjection(context.Context, state.OpaqueIngressResolutionRequest) (state.OpaqueIngressResolvedProjection, error)
}

// StoreResolver turns a trusted body-blind ingress envelope into the exact
// Admission request authority pinned by the active projection.
type StoreResolver struct {
	store ProjectionStore
}

func NewStoreResolver(store ProjectionStore) (*StoreResolver, error) {
	if store == nil {
		return nil, errors.New("opaque HTTP projection store is required")
	}
	return &StoreResolver{store: store}, nil
}

func (r *StoreResolver) ResolveOpaqueHTTPInvocation(ctx context.Context, request ResolutionRequest) (ResolvedAdmission, error) {
	trusted := request.TrustedIngress
	resolved, err := r.store.ResolveOpaqueIngressProjection(ctx, state.OpaqueIngressResolutionRequest{
		Issuer:             trusted.Issuer,
		Audience:           trusted.Audience,
		PublicationRef:     trusted.PublicationRef,
		RouteGeneration:    trusted.RouteGeneration,
		CredentialID:       trusted.CredentialRef.ID,
		CredentialRevision: trusted.CredentialRef.Revision,
		Method:             request.HTTP.Method,
		ExactEscapedPath:   request.HTTP.ExactEscapedPath,
		ContentType:        request.HTTP.ContentType,
		BodyByteLength:     request.BodyByteLength,
	})
	if err != nil {
		return ResolvedAdmission{}, opaqueHTTPProjectionUnavailable()
	}
	if err := validateAtomicOpaqueIngressProjection(request, resolved); err != nil {
		return ResolvedAdmission{}, opaqueHTTPProjectionUnavailable()
	}

	publication := resolved.Publication
	credential := resolved.Credential
	references := make([]contract.NamedImmutableReferencePin, 0, 1+len(publication.References)+len(credential.References))
	references = append(references, contract.NamedImmutableReferencePin{
		Name: opaqueHTTPPublicationPinName,
		Reference: contract.ImmutableReference{
			ID:      publication.PublicationRef,
			Version: publication.Digest,
		},
	})
	references = append(references, cloneNamedImmutableReferences(publication.References)...)
	references = append(references, cloneNamedImmutableReferences(credential.References)...)

	principalID := "opaque-ingress-" + strings.TrimPrefix(credential.Reference.Digest, "sha256:")
	principal := execution.Principal{
		Kind:           execution.PrincipalService,
		ID:             principalID,
		Workspace:      publication.WorkspaceID,
		Subject:        "service:" + principalID,
		Scopes:         []execution.Scope{execution.ScopeRunsCreate, execution.ScopeRunsReadOwn},
		AllowedTargets: []string{publication.App + "/" + publication.Action},
	}.Normalized()

	return ResolvedAdmission{
		Workspace: publication.WorkspaceID,
		App:       publication.App,
		Action:    publication.Action,
		ExpectedRelease: execution.ActiveReleasePrecondition{
			DeploymentID: publication.Release.DeploymentID,
			Commit:       publication.Release.Commit,
			BundleDigest: publication.Release.BundleDigest,
		},
		Principal: principal,
		InvocationPins: contract.InvocationPins{
			PublicationRef:  publication.PublicationRef,
			RouteGeneration: resolved.Activation.Generation,
			OperationRef:    publication.OperationRef,
			CredentialRef: contract.ImmutableReference{
				ID:      credential.Reference.ID,
				Version: credential.Reference.Revision,
			},
			References: references,
		},
		ResponsePolicy: contract.CloneHTTPPolicy(publication.HTTP.ResponsePolicy),
	}, nil
}

func validateAtomicOpaqueIngressProjection(request ResolutionRequest, resolved state.OpaqueIngressResolvedProjection) error {
	trusted := request.TrustedIngress
	publication := resolved.Publication
	activation := resolved.Activation
	credential := resolved.Credential

	workspace := contract.NormalizeWorkspace(publication.WorkspaceID)
	if workspace == "" || publication.WorkspaceID != workspace ||
		publication.Issuer != trusted.Issuer || publication.Audience != trusted.Audience || publication.PublicationRef != trusted.PublicationRef ||
		activation.WorkspaceID != publication.WorkspaceID || activation.Issuer != publication.Issuer || activation.Audience != publication.Audience || activation.PublicationRef != publication.PublicationRef ||
		activation.Generation != trusted.RouteGeneration || activation.State != state.OpaqueIngressActivationActive ||
		activation.Revision != publication.Revision || activation.PublicationDigest != publication.Digest ||
		credential.WorkspaceID != publication.WorkspaceID || credential.Issuer != publication.Issuer || credential.Audience != publication.Audience ||
		credential.Reference.ID != trusted.CredentialRef.ID || credential.Reference.Revision != trusted.CredentialRef.Revision {
		return errors.New("mixed opaque ingress projection")
	}
	if publication.Digest == "" || publication.Digest != state.OpaqueIngressPublicationRevisionDigest(publication) ||
		credential.Reference.Digest == "" || credential.Reference.Digest != state.OpaqueIngressCredentialSnapshotDigest(credential) {
		return errors.New("opaque ingress projection digest mismatch")
	}
	if !contract.ValidAppKey(publication.App) || !contract.ValidActionKey(publication.Action) ||
		strings.TrimSpace(publication.Release.DeploymentID) == "" || strings.TrimSpace(publication.Release.Commit) == "" || strings.TrimSpace(publication.Release.BundleDigest) == "" ||
		len(publication.OperationRef) == 0 || len(publication.OperationRef) > 200 || !operationRefPattern.MatchString(publication.OperationRef) ||
		publication.OperationRef != credential.OperationRef {
		return errors.New("incomplete opaque ingress projection")
	}
	if publication.HTTP.Method != request.HTTP.Method || publication.HTTP.ExactEscapedPath != request.HTTP.ExactEscapedPath ||
		publication.HTTP.ContentType != request.HTTP.ContentType || request.BodyByteLength < 0 || request.BodyByteLength > publication.HTTP.MaxRequestBodyBytes {
		return errors.New("opaque ingress HTTP contract mismatch")
	}

	matchingCredentialRefs := 0
	for _, reference := range publication.CredentialRefs {
		if reference.ID == credential.Reference.ID && reference.Revision == credential.Reference.Revision && reference.Digest == credential.Reference.Digest {
			matchingCredentialRefs++
		}
	}
	if matchingCredentialRefs != 1 {
		return errors.New("opaque ingress credential snapshot is not pinned exactly once")
	}
	if err := validateResolverReferences(publication.References, credential.References); err != nil {
		return err
	}
	if err := validateResolvedResponsePolicy(publication.HTTP.ResponsePolicy, contract.MaxApplicationWireResponseBodyBytes); err != nil {
		return err
	}
	return nil
}

func validateResolverReferences(groups ...[]contract.NamedImmutableReferencePin) error {
	seen := map[string]struct{}{opaqueHTTPPublicationPinName: {}}
	total := 1
	for _, group := range groups {
		total += len(group)
		if total > 32 {
			return errors.New("too many opaque ingress invocation references")
		}
		for _, pin := range group {
			if len(pin.Name) == 0 || len(pin.Name) > 80 || !pinNamePattern.MatchString(pin.Name) {
				return errors.New("invalid opaque ingress invocation reference name")
			}
			if _, duplicate := seen[pin.Name]; duplicate {
				return fmt.Errorf("duplicate opaque ingress invocation reference %q", pin.Name)
			}
			seen[pin.Name] = struct{}{}
			if err := validateTrimmedString("opaque ingress invocation reference id", pin.Reference.ID, 200); err != nil {
				return err
			}
			if err := validateTrimmedString("opaque ingress invocation reference version", pin.Reference.Version, 200); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneNamedImmutableReferences(input []contract.NamedImmutableReferencePin) []contract.NamedImmutableReferencePin {
	if len(input) == 0 {
		return nil
	}
	return append([]contract.NamedImmutableReferencePin(nil), input...)
}

func opaqueHTTPProjectionUnavailable() error {
	return &ResolutionFailure{Category: FailureCapacityUnavailable, Retryable: false}
}
