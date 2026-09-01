package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) PutOpaqueIngressCredentialSnapshot(ctx context.Context, request OpaqueIngressCredentialSnapshotRequest) (OpaqueIngressCredentialSnapshot, bool, error) {
	request, err := normalizeOpaqueIngressCredentialRequest(request)
	if err != nil {
		return OpaqueIngressCredentialSnapshot{}, false, err
	}
	var result OpaqueIngressCredentialSnapshot
	var replay bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if existing, ok, err := localOpaqueIngressReplay[OpaqueIngressCredentialSnapshot](snapshot, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "credential_snapshot"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return errSkipLocalStateWrite
		}
		if _, ok := snapshot.Workspaces[request.WorkspaceID]; !ok {
			return ErrNotFound
		}
		key := opaqueIngressCredentialKey(request.Issuer, request.Audience, request.Reference.ID, request.Reference.Revision)
		if _, exists := snapshot.OpaqueIngressCredentials[key]; exists {
			return fmt.Errorf("%w: credential snapshot already exists", ErrConflict)
		}
		result = OpaqueIngressCredentialSnapshot{
			WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
			Reference: request.Reference, OperationRef: request.OperationRef,
			References:  append([]contract.NamedImmutableReferencePin(nil), request.References...),
			ProjectedAt: request.ProjectedAt.UTC(), NotAfter: request.NotAfter.UTC(),
			MaxStalenessSeconds: request.MaxStalenessSeconds, OperationID: request.OperationID,
			RequestFingerprint: request.RequestFingerprint, Actor: request.Actor, CreatedAt: now,
		}
		snapshot.OpaqueIngressCredentials[key] = result
		if err := recordLocalOpaqueIngressOperation(snapshot, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "credential_snapshot", result, now); err != nil {
			return err
		}
		appendLocalOpaqueIngressAudit(snapshot, OpaqueIngressAudit{
			WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			SubjectKind: "credential_snapshot", SubjectID: result.Reference.ID + "/" + result.Reference.Revision,
			Kind: "created", OperationID: result.OperationID, Actor: result.Actor,
		}, now)
		return nil
	})
	return result, replay, err
}

func (s *LocalStore) RevokeOpaqueIngressCredentialSnapshot(ctx context.Context, request OpaqueIngressCredentialRevocationRequest) (OpaqueIngressCredentialRevocation, bool, error) {
	normalized, err := normalizeOpaqueIngressRevocationRequest(request)
	if err != nil {
		return OpaqueIngressCredentialRevocation{}, false, err
	}
	request = normalized
	var result OpaqueIngressCredentialRevocation
	var replay bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if existing, ok, err := localOpaqueIngressReplay[OpaqueIngressCredentialRevocation](snapshot, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "credential_revocation"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return errSkipLocalStateWrite
		}
		key := opaqueIngressCredentialKey(request.Issuer, request.Audience, request.Reference.ID, request.Reference.Revision)
		credential, exists := snapshot.OpaqueIngressCredentials[key]
		if !exists || credential.WorkspaceID != request.WorkspaceID ||
			!opaqueIngressCredentialRefEqual(credential.Reference, request.Reference) ||
			credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) {
			return ErrNotFound
		}
		if _, exists := snapshot.OpaqueIngressRevocations[key]; exists {
			return fmt.Errorf("%w: credential snapshot is already revoked", ErrConflict)
		}
		result = OpaqueIngressCredentialRevocation{
			ID: NewID("opaque-revocation"), WorkspaceID: request.WorkspaceID,
			Issuer: request.Issuer, Audience: request.Audience, Reference: request.Reference,
			Reason: request.Reason, OperationID: request.OperationID,
			RequestFingerprint: request.RequestFingerprint, Actor: request.Actor, CreatedAt: now,
		}
		snapshot.OpaqueIngressRevocations[key] = result
		if err := recordLocalOpaqueIngressOperation(snapshot, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "credential_revocation", result, now); err != nil {
			return err
		}
		appendLocalOpaqueIngressAudit(snapshot, OpaqueIngressAudit{
			WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			SubjectKind: "credential_snapshot", SubjectID: result.Reference.ID + "/" + result.Reference.Revision,
			Kind: "revoked", Detail: result.Reason, OperationID: result.OperationID, Actor: result.Actor,
		}, now)
		return nil
	})
	return result, replay, err
}

func (s *LocalStore) PutOpaqueIngressPublicationRevision(ctx context.Context, request OpaqueIngressPublicationRevisionRequest) (OpaqueIngressPublicationRevision, bool, error) {
	request, err := normalizeOpaqueIngressPublicationRequest(request)
	if err != nil {
		return OpaqueIngressPublicationRevision{}, false, err
	}
	var result OpaqueIngressPublicationRevision
	var replay bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if existing, ok, err := localOpaqueIngressReplay[OpaqueIngressPublicationRevision](snapshot, request.Revision.WorkspaceID, request.OperationID, request.RequestFingerprint, "publication_revision"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return errSkipLocalStateWrite
		}
		if _, ok := snapshot.Workspaces[request.Revision.WorkspaceID]; !ok {
			return ErrNotFound
		}
		for _, reference := range request.Revision.CredentialRefs {
			key := opaqueIngressCredentialKey(request.Revision.Issuer, request.Revision.Audience, reference.ID, reference.Revision)
			credential, exists := snapshot.OpaqueIngressCredentials[key]
			if !exists || credential.WorkspaceID != request.Revision.WorkspaceID ||
				!opaqueIngressCredentialRefEqual(credential.Reference, reference) ||
				credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) {
				return fmt.Errorf("%w: credential snapshot is missing or mixed", ErrOpaqueIngressProjectionRejected)
			}
			if _, revoked := snapshot.OpaqueIngressRevocations[key]; revoked {
				return fmt.Errorf("%w: credential snapshot is revoked", ErrOpaqueIngressProjectionRejected)
			}
			if credential.OperationRef != request.Revision.OperationRef || validateOpaqueIngressCombinedReferences(request.Revision.References, credential.References) != nil {
				return fmt.Errorf("%w: credential operation or references are mixed", ErrOpaqueIngressProjectionRejected)
			}
		}
		key := opaqueIngressPublicationKey(request.Revision.Issuer, request.Revision.Audience, request.Revision.PublicationRef, request.Revision.Revision)
		if _, exists := snapshot.OpaqueIngressPublications[key]; exists {
			return fmt.Errorf("%w: publication revision already exists", ErrConflict)
		}
		result = cloneOpaqueIngressPublication(request.Revision)
		result.CreatedAt = now
		snapshot.OpaqueIngressPublications[key] = result
		if err := recordLocalOpaqueIngressOperation(snapshot, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "publication_revision", result, now); err != nil {
			return err
		}
		appendLocalOpaqueIngressAudit(snapshot, OpaqueIngressAudit{
			WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			PublicationRef: result.PublicationRef, SubjectKind: "publication_revision", SubjectID: result.Revision,
			Kind: "created", OperationID: result.OperationID, Actor: result.Actor,
		}, now)
		return nil
	})
	return cloneOpaqueIngressPublication(result), replay, err
}

func (s *LocalStore) ActivateOpaqueIngressPublication(ctx context.Context, request OpaqueIngressActivationRequest) (OpaqueIngressActivation, bool, error) {
	request, err := normalizeOpaqueIngressActivationRequest(request)
	if err != nil {
		return OpaqueIngressActivation{}, false, err
	}
	var result OpaqueIngressActivation
	var replay bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if existing, ok, err := localOpaqueIngressReplay[OpaqueIngressActivation](snapshot, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "activation"); err != nil {
			return err
		} else if ok {
			if existing.AuthorizedTarget != request.AuthorizedTarget {
				return fmt.Errorf("%w: operation authorization target changed", ErrConflict)
			}
			result, replay = existing, true
			return errSkipLocalStateWrite
		}
		headKey := opaqueIngressHeadKey(request.Issuer, request.Audience, request.PublicationRef)
		head, hasHead := snapshot.OpaqueIngressHeads[headKey]
		currentGeneration := int64(0)
		if hasHead {
			if head.WorkspaceID != request.WorkspaceID {
				return fmt.Errorf("%w: publication trust key belongs to another workspace", ErrConflict)
			}
			currentGeneration = head.Generation
		}
		if currentGeneration != request.ExpectedGeneration {
			return fmt.Errorf("%w: expected generation %d, current generation %d", ErrConflict, request.ExpectedGeneration, currentGeneration)
		}
		stateValue := OpaqueIngressActivationActive
		targetRevision := request.TargetRevision
		if request.Kind == OpaqueIngressActivationKindRevoke {
			if !hasHead {
				return fmt.Errorf("%w: no publication is active", ErrInvalidState)
			}
			targetRevision = head.Revision
			stateValue = OpaqueIngressActivationRevoked
		} else if request.Kind == OpaqueIngressActivationKindRollback && !hasHead {
			return fmt.Errorf("%w: rollback requires an existing head", ErrInvalidState)
		}
		publicationKey := opaqueIngressPublicationKey(request.Issuer, request.Audience, request.PublicationRef, targetRevision)
		publication, exists := snapshot.OpaqueIngressPublications[publicationKey]
		if !exists || publication.WorkspaceID != request.WorkspaceID {
			return ErrNotFound
		}
		if publication.Digest != OpaqueIngressPublicationRevisionDigest(publication) {
			return fmt.Errorf("%w: publication digest mismatch", ErrOpaqueIngressProjectionRejected)
		}
		if request.AuthorizedTarget != "" && request.AuthorizedTarget != publication.App+"/"+publication.Action {
			return fmt.Errorf("%w: activation target is not authorized", ErrOpaqueIngressProjectionRejected)
		}
		if stateValue == OpaqueIngressActivationActive {
			if err := validateLocalOpaqueIngressPublication(snapshot, publication, now); err != nil {
				return err
			}
		}
		result = OpaqueIngressActivation{
			WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
			PublicationRef: request.PublicationRef, Generation: currentGeneration + 1,
			Revision: publication.Revision, PublicationDigest: publication.Digest,
			State: stateValue, Kind: request.Kind, AuthorizedTarget: request.AuthorizedTarget,
			OperationID:        request.OperationID,
			RequestFingerprint: request.RequestFingerprint, Actor: request.Actor, CreatedAt: now,
		}
		activationKey := opaqueIngressActivationKey(request.Issuer, request.Audience, request.PublicationRef, result.Generation)
		snapshot.OpaqueIngressActivations[activationKey] = result
		snapshot.OpaqueIngressHeads[headKey] = OpaqueIngressProjectionHead{
			WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			PublicationRef: result.PublicationRef, Generation: result.Generation,
			Revision: result.Revision, PublicationDigest: result.PublicationDigest,
			State: result.State, UpdatedBy: result.Actor, UpdatedAt: now,
		}
		if err := recordLocalOpaqueIngressOperation(snapshot, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "activation", result, now); err != nil {
			return err
		}
		appendLocalOpaqueIngressAudit(snapshot, OpaqueIngressAudit{
			WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			PublicationRef: result.PublicationRef, Generation: result.Generation,
			SubjectKind: "publication", SubjectID: result.Revision, Kind: result.Kind,
			OperationID: result.OperationID, Actor: result.Actor,
		}, now)
		return nil
	})
	return result, replay, err
}

func (s *LocalStore) ResolveOpaqueIngressProjection(ctx context.Context, request OpaqueIngressResolutionRequest) (OpaqueIngressResolvedProjection, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	return resolveLocalOpaqueIngressProjection(&snapshot, request)
}

func (s *LocalStore) ListOpaqueIngressProjectionAudit(ctx context.Context, workspaceID string, publicationRef string, limit int) ([]OpaqueIngressAudit, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	publicationRef = strings.TrimSpace(publicationRef)
	if limit <= 0 || limit > 1000 || (publicationRef != "" && (len(publicationRef) > 100 || !opaqueIngressPublicationRefPattern.MatchString(publicationRef))) {
		return nil, fmt.Errorf("%w: audit limit must be between 1 and 1000", ErrInvalidState)
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := snapshot.OpaqueIngressAudits[workspaceID]
	result := make([]OpaqueIngressAudit, 0, min(limit, len(records)))
	for index := len(records) - 1; index >= 0 && len(result) < limit; index-- {
		if publicationRef != "" && records[index].PublicationRef != publicationRef {
			continue
		}
		result = append(result, records[index])
	}
	return result, nil
}

func (s *LocalStore) PruneOpaqueIngressProjectionHistory(ctx context.Context, request OpaqueIngressRetentionRequest) (OpaqueIngressRetentionResult, bool, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.Before = request.Before.UTC()
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	if request.Before.IsZero() || request.Limit <= 0 || request.Limit > 1000 ||
		!validOpaqueIngressString(request.OperationID, 200) || !validOpaqueIngressString(request.RequestFingerprint, 256) ||
		!validOpaqueIngressString(request.Actor, 256) {
		return OpaqueIngressRetentionResult{}, false, fmt.Errorf("%w: retention cutoff and limit are required", ErrInvalidState)
	}
	var result OpaqueIngressRetentionResult
	var replay bool
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		if existing, ok, err := localOpaqueIngressReplay[OpaqueIngressRetentionResult](snapshot, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "retention"); err != nil {
			return err
		} else if ok {
			result = existing
			replay = true
			return errSkipLocalStateWrite
		}
		if _, ok := snapshot.Workspaces[request.WorkspaceID]; !ok {
			return ErrNotFound
		}
		activated := map[string]struct{}{}
		for _, activation := range snapshot.OpaqueIngressActivations {
			activated[opaqueIngressPublicationKey(activation.Issuer, activation.Audience, activation.PublicationRef, activation.Revision)] = struct{}{}
		}
		publicationKeys := sortedOpaqueIngressKeys(snapshot.OpaqueIngressPublications)
		remaining := request.Limit
		for _, key := range publicationKeys {
			publication := snapshot.OpaqueIngressPublications[key]
			if remaining == 0 || publication.WorkspaceID != request.WorkspaceID || !publication.CreatedAt.Before(request.Before) || publication.NotAfter.After(request.Before) || publication.RetainUntil.After(request.Before) {
				continue
			}
			if _, retained := activated[key]; retained {
				continue
			}
			delete(snapshot.OpaqueIngressPublications, key)
			result.PublicationRevisions++
			remaining--
		}
		referenced := map[string]struct{}{}
		for _, publication := range snapshot.OpaqueIngressPublications {
			for _, reference := range publication.CredentialRefs {
				referenced[opaqueIngressCredentialKey(publication.Issuer, publication.Audience, reference.ID, reference.Revision)] = struct{}{}
			}
		}
		credentialKeys := sortedOpaqueIngressKeys(snapshot.OpaqueIngressCredentials)
		for _, key := range credentialKeys {
			credential := snapshot.OpaqueIngressCredentials[key]
			if remaining == 0 || credential.WorkspaceID != request.WorkspaceID || !credential.CreatedAt.Before(request.Before) || credential.NotAfter.After(request.Before) {
				continue
			}
			if _, retained := referenced[key]; retained {
				continue
			}
			if _, revoked := snapshot.OpaqueIngressRevocations[key]; revoked {
				continue
			}
			delete(snapshot.OpaqueIngressCredentials, key)
			result.CredentialSnapshots++
			remaining--
		}
		if err := recordLocalOpaqueIngressOperation(snapshot, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "retention", result, now); err != nil {
			return err
		}
		appendLocalOpaqueIngressAudit(snapshot, OpaqueIngressAudit{
			WorkspaceID: request.WorkspaceID, SubjectKind: "retention", SubjectID: request.WorkspaceID,
			Kind: "pruned", Detail: fmt.Sprintf("publication_revisions=%d credential_snapshots=%d", result.PublicationRevisions, result.CredentialSnapshots),
			OperationID: request.OperationID, Actor: request.Actor,
		}, now)
		return nil
	})
	return result, replay, err
}

func resolveLocalOpaqueIngressProjection(snapshot *Snapshot, request OpaqueIngressResolutionRequest) (OpaqueIngressResolvedProjection, error) {
	if request.Now.IsZero() {
		request.Now = time.Now().UTC()
	} else {
		request.Now = request.Now.UTC()
	}
	request.Issuer = strings.TrimSpace(request.Issuer)
	request.Audience = strings.TrimSpace(request.Audience)
	request.PublicationRef = strings.TrimSpace(request.PublicationRef)
	request.CredentialID = strings.TrimSpace(request.CredentialID)
	request.CredentialRevision = strings.TrimSpace(request.CredentialRevision)
	request.Method = strings.ToUpper(strings.TrimSpace(request.Method))
	request.ExactEscapedPath = strings.TrimSpace(request.ExactEscapedPath)
	request.ContentType = strings.TrimSpace(request.ContentType)
	if !validOpaqueIngressString(request.Issuer, 160) || !validOpaqueIngressString(request.Audience, 160) ||
		len(request.PublicationRef) > 100 || !opaqueIngressPublicationRefPattern.MatchString(request.PublicationRef) ||
		request.RouteGeneration < 1 || !validOpaqueIngressString(request.CredentialID, 200) ||
		!opaqueIngressSHA256Pattern.MatchString(request.CredentialRevision) || !validOpaqueIngressHTTPMethod(request.Method) ||
		!validOpaqueIngressPath(request.ExactEscapedPath) || !validOpaqueIngressMediaType(request.ContentType) || request.BodyByteLength < 0 {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	headKey := opaqueIngressHeadKey(request.Issuer, request.Audience, request.PublicationRef)
	head, ok := snapshot.OpaqueIngressHeads[headKey]
	if !ok || head.Issuer != request.Issuer || head.Audience != request.Audience || head.PublicationRef != request.PublicationRef || head.Generation != request.RouteGeneration || head.State != OpaqueIngressActivationActive {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	activation, ok := snapshot.OpaqueIngressActivations[opaqueIngressActivationKey(request.Issuer, request.Audience, request.PublicationRef, request.RouteGeneration)]
	if !ok || activation.Issuer != request.Issuer || activation.Audience != request.Audience || activation.PublicationRef != request.PublicationRef || activation.Generation != request.RouteGeneration || activation.State != OpaqueIngressActivationActive || activation.Revision != head.Revision || activation.PublicationDigest != head.PublicationDigest || activation.WorkspaceID != head.WorkspaceID {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	publication, ok := snapshot.OpaqueIngressPublications[opaqueIngressPublicationKey(request.Issuer, request.Audience, request.PublicationRef, head.Revision)]
	if !ok || publication.WorkspaceID != head.WorkspaceID || publication.Issuer != request.Issuer || publication.Audience != request.Audience || publication.PublicationRef != request.PublicationRef || publication.Digest != head.PublicationDigest || publication.Digest != OpaqueIngressPublicationRevisionDigest(publication) {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if publication.HTTP.Method != request.Method || publication.HTTP.ExactEscapedPath != request.ExactEscapedPath || publication.HTTP.ContentType != request.ContentType || request.BodyByteLength > publication.HTTP.MaxRequestBodyBytes {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	var reference OpaqueIngressCredentialSnapshotRef
	found := false
	for _, candidate := range publication.CredentialRefs {
		if candidate.ID == request.CredentialID && candidate.Revision == request.CredentialRevision {
			reference, found = candidate, true
			break
		}
	}
	if !found {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	credentialKey := opaqueIngressCredentialKey(request.Issuer, request.Audience, request.CredentialID, request.CredentialRevision)
	credential, ok := snapshot.OpaqueIngressCredentials[credentialKey]
	if !ok || credential.WorkspaceID != publication.WorkspaceID || credential.Issuer != request.Issuer || credential.Audience != request.Audience || !opaqueIngressCredentialRefEqual(credential.Reference, reference) || credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if _, revoked := snapshot.OpaqueIngressRevocations[credentialKey]; revoked {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if publication.OperationRef != credential.OperationRef || validateOpaqueIngressCombinedReferences(publication.References, credential.References) != nil {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if err := validateOpaqueIngressFreshAt(publication.ProjectedAt, publication.NotAfter, publication.MaxStalenessSeconds, request.Now); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := validateOpaqueIngressFreshAt(credential.ProjectedAt, credential.NotAfter, credential.MaxStalenessSeconds, request.Now); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := validateLocalOpaqueIngressRelease(snapshot, publication); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	return OpaqueIngressResolvedProjection{Publication: cloneOpaqueIngressPublication(publication), Activation: activation, Credential: credential}, nil
}

func validateLocalOpaqueIngressPublication(snapshot *Snapshot, publication OpaqueIngressPublicationRevision, now time.Time) error {
	if err := validateLocalOpaqueIngressRelease(snapshot, publication); err != nil {
		return err
	}
	if err := validateOpaqueIngressFreshAt(publication.ProjectedAt, publication.NotAfter, publication.MaxStalenessSeconds, now); err != nil {
		return err
	}
	for _, reference := range publication.CredentialRefs {
		key := opaqueIngressCredentialKey(publication.Issuer, publication.Audience, reference.ID, reference.Revision)
		credential, ok := snapshot.OpaqueIngressCredentials[key]
		if !ok || credential.WorkspaceID != publication.WorkspaceID ||
			!opaqueIngressCredentialRefEqual(credential.Reference, reference) ||
			credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) {
			return ErrOpaqueIngressProjectionRejected
		}
		if _, revoked := snapshot.OpaqueIngressRevocations[key]; revoked {
			return ErrOpaqueIngressProjectionRejected
		}
		if credential.OperationRef != publication.OperationRef || validateOpaqueIngressCombinedReferences(publication.References, credential.References) != nil {
			return ErrOpaqueIngressProjectionRejected
		}
		if err := validateOpaqueIngressFreshAt(credential.ProjectedAt, credential.NotAfter, credential.MaxStalenessSeconds, now); err != nil {
			return err
		}
	}
	return nil
}

func validateLocalOpaqueIngressRelease(snapshot *Snapshot, publication OpaqueIngressPublicationRevision) error {
	deployment, ok := snapshot.ReleaseCatalog.Deployments[catalog.DeploymentKey(publication.WorkspaceID, publication.App)]
	if !ok || !opaqueIngressReleaseMatches(publication.Release, deployment.DeploymentID, deployment.Commit, deployment.BundleDigest) {
		return fmt.Errorf("%w: active Release does not match", ErrOpaqueIngressProjectionRejected)
	}
	return nil
}

func normalizeOpaqueIngressActivationRequest(request OpaqueIngressActivationRequest) (OpaqueIngressActivationRequest, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.Issuer = strings.TrimSpace(request.Issuer)
	request.Audience = strings.TrimSpace(request.Audience)
	request.PublicationRef = strings.TrimSpace(request.PublicationRef)
	request.TargetRevision = strings.TrimSpace(request.TargetRevision)
	request.Kind = strings.TrimSpace(request.Kind)
	request.AuthorizedTarget = strings.TrimSpace(request.AuthorizedTarget)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	if !validOpaqueIngressString(request.Issuer, 160) || !validOpaqueIngressString(request.Audience, 160) ||
		len(request.PublicationRef) > 100 || !opaqueIngressPublicationRefPattern.MatchString(request.PublicationRef) ||
		request.ExpectedGeneration < 0 || !validOpaqueIngressString(request.OperationID, 200) ||
		!validOpaqueIngressString(request.RequestFingerprint, 256) || !validOpaqueIngressString(request.Actor, 256) ||
		(request.AuthorizedTarget != "" && len(request.AuthorizedTarget) > 260) {
		return request, fmt.Errorf("%w: activation fields are invalid", ErrInvalidState)
	}
	if request.Kind != OpaqueIngressActivationKindActivate && request.Kind != OpaqueIngressActivationKindRollback && request.Kind != OpaqueIngressActivationKindRevoke {
		return request, fmt.Errorf("%w: activation kind is invalid", ErrInvalidState)
	}
	if request.Kind != OpaqueIngressActivationKindRevoke && request.TargetRevision == "" {
		return request, fmt.Errorf("%w: target revision is required", ErrInvalidState)
	}
	return request, nil
}

func normalizeOpaqueIngressRevocationRequest(request OpaqueIngressCredentialRevocationRequest) (OpaqueIngressCredentialRevocationRequest, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.Issuer = strings.TrimSpace(request.Issuer)
	request.Audience = strings.TrimSpace(request.Audience)
	request.Reference.ID = strings.TrimSpace(request.Reference.ID)
	request.Reference.Revision = strings.TrimSpace(request.Reference.Revision)
	request.Reference.Digest = strings.TrimSpace(request.Reference.Digest)
	request.Reason = strings.TrimSpace(request.Reason)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	if !validOpaqueIngressString(request.Issuer, 160) || !validOpaqueIngressString(request.Audience, 160) ||
		!validOpaqueIngressString(request.Reference.ID, 200) || !opaqueIngressSHA256Pattern.MatchString(request.Reference.Revision) ||
		!opaqueIngressSHA256Pattern.MatchString(request.Reference.Digest) || len(request.Reason) > 1000 ||
		!validOpaqueIngressString(request.OperationID, 200) || !validOpaqueIngressString(request.RequestFingerprint, 256) ||
		!validOpaqueIngressString(request.Actor, 256) {
		return request, fmt.Errorf("%w: credential revocation fields are required", ErrInvalidState)
	}
	return request, nil
}

func localOpaqueIngressReplay[T any](snapshot *Snapshot, workspaceID, operationID, fingerprint, kind string) (T, bool, error) {
	var zero T
	operation, ok := snapshot.OpaqueIngressOperations[opaqueIngressOperationKey(workspaceID, operationID)]
	if !ok {
		return zero, false, nil
	}
	if operation.RequestFingerprint != fingerprint || operation.Kind != kind {
		return zero, false, fmt.Errorf("%w: operation ID was reused with another request", ErrConflict)
	}
	if err := json.Unmarshal(operation.Result, &zero); err != nil {
		return zero, false, fmt.Errorf("decode opaque ingress operation replay: %w", err)
	}
	return zero, true, nil
}

func recordLocalOpaqueIngressOperation(snapshot *Snapshot, workspaceID, operationID, fingerprint, kind string, result any, now time.Time) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	snapshot.OpaqueIngressOperations[opaqueIngressOperationKey(workspaceID, operationID)] = OpaqueIngressOperation{
		WorkspaceID: workspaceID, OperationID: operationID, RequestFingerprint: fingerprint,
		Kind: kind, Result: raw, CreatedAt: now,
	}
	return nil
}

func appendLocalOpaqueIngressAudit(snapshot *Snapshot, record OpaqueIngressAudit, now time.Time) {
	record.ID = NewID("opaque-audit")
	record.CreatedAt = now
	snapshot.OpaqueIngressAudits[record.WorkspaceID] = append(snapshot.OpaqueIngressAudits[record.WorkspaceID], record)
}

func opaqueIngressCredentialKey(issuer, audience, id, revision string) string {
	return strings.Join([]string{issuer, audience, id, revision}, "\x1f")
}

func opaqueIngressPublicationKey(issuer, audience, publicationRef, revision string) string {
	return strings.Join([]string{issuer, audience, publicationRef, revision}, "\x1f")
}

func opaqueIngressHeadKey(issuer, audience, publicationRef string) string {
	return strings.Join([]string{issuer, audience, publicationRef}, "\x1f")
}

func opaqueIngressActivationKey(issuer, audience, publicationRef string, generation int64) string {
	return fmt.Sprintf("%s\x1f%d", opaqueIngressHeadKey(issuer, audience, publicationRef), generation)
}

func opaqueIngressOperationKey(workspaceID, operationID string) string {
	return contract.NormalizeWorkspace(workspaceID) + "\x1f" + operationID
}

func sortedOpaqueIngressKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
