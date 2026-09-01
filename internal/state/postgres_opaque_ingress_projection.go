package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *PostgresStore) PutOpaqueIngressCredentialSnapshot(ctx context.Context, request OpaqueIngressCredentialSnapshotRequest) (OpaqueIngressCredentialSnapshot, bool, error) {
	request, err := normalizeOpaqueIngressCredentialRequest(request)
	if err != nil {
		return OpaqueIngressCredentialSnapshot{}, false, err
	}
	var result OpaqueIngressCredentialSnapshot
	var replay bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := postgresOpaqueIngressLock(ctx, tx, "credential", request.Issuer, request.Audience, request.Reference.ID, request.Reference.Revision); err != nil {
			return err
		}
		if existing, ok, err := postgresOpaqueIngressReplay[OpaqueIngressCredentialSnapshot](ctx, tx, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "credential_snapshot"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return nil
		}
		if err := requirePostgresOpaqueIngressWorkspace(ctx, tx, request.WorkspaceID); err != nil {
			return err
		}
		now := time.Now().UTC()
		result = OpaqueIngressCredentialSnapshot{
			WorkspaceID: request.WorkspaceID, Issuer: request.Issuer, Audience: request.Audience,
			Reference: request.Reference, OperationRef: request.OperationRef,
			References:  append([]contract.NamedImmutableReferencePin(nil), request.References...),
			ProjectedAt: request.ProjectedAt.UTC(), NotAfter: request.NotAfter.UTC(),
			MaxStalenessSeconds: request.MaxStalenessSeconds, OperationID: request.OperationID,
			RequestFingerprint: request.RequestFingerprint, Actor: request.Actor, CreatedAt: now,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_credential_snapshot (
    workspace_id, issuer, audience, credential_id, credential_revision, digest, record, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
`, result.WorkspaceID, result.Issuer, result.Audience, result.Reference.ID, result.Reference.Revision, result.Reference.Digest, raw, result.CreatedAt); err != nil {
			return opaqueIngressPostgresConflict(err)
		}
		if err := insertPostgresOpaqueIngressOperation(ctx, tx, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "credential_snapshot", raw, now); err != nil {
			return err
		}
		return insertPostgresOpaqueIngressAudit(ctx, tx, OpaqueIngressAudit{
			ID: NewID("opaque-audit"), WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			SubjectKind: "credential_snapshot", SubjectID: result.Reference.ID + "/" + result.Reference.Revision,
			Kind: "created", OperationID: result.OperationID, Actor: result.Actor, CreatedAt: now,
		})
	})
	return cloneOpaqueIngressCredential(result), replay, err
}

func (s *PostgresStore) RevokeOpaqueIngressCredentialSnapshot(ctx context.Context, request OpaqueIngressCredentialRevocationRequest) (OpaqueIngressCredentialRevocation, bool, error) {
	request, err := normalizeOpaqueIngressRevocationRequest(request)
	if err != nil {
		return OpaqueIngressCredentialRevocation{}, false, err
	}
	var result OpaqueIngressCredentialRevocation
	var replay bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := postgresOpaqueIngressLock(ctx, tx, "credential", request.Issuer, request.Audience, request.Reference.ID, request.Reference.Revision); err != nil {
			return err
		}
		if existing, ok, err := postgresOpaqueIngressReplay[OpaqueIngressCredentialRevocation](ctx, tx, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "credential_revocation"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return nil
		}
		if err := requirePostgresOpaqueIngressWorkspace(ctx, tx, request.WorkspaceID); err != nil {
			return err
		}
		var rawCredential []byte
		if err := tx.QueryRow(ctx, `
SELECT record FROM opaque_ingress_credential_snapshot
WHERE issuer=$1 AND audience=$2 AND credential_id=$3 AND credential_revision=$4 AND workspace_id=$5
FOR UPDATE
`, request.Issuer, request.Audience, request.Reference.ID, request.Reference.Revision, request.WorkspaceID).Scan(&rawCredential); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var credential OpaqueIngressCredentialSnapshot
		if err := json.Unmarshal(rawCredential, &credential); err != nil {
			return err
		}
		if !opaqueIngressCredentialRefEqual(credential.Reference, request.Reference) ||
			credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) {
			return ErrOpaqueIngressProjectionRejected
		}
		now := time.Now().UTC()
		result = OpaqueIngressCredentialRevocation{
			ID: NewID("opaque-revocation"), WorkspaceID: request.WorkspaceID,
			Issuer: request.Issuer, Audience: request.Audience, Reference: request.Reference,
			Reason: request.Reason, OperationID: request.OperationID,
			RequestFingerprint: request.RequestFingerprint, Actor: request.Actor, CreatedAt: now,
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_credential_revocation (
    id, workspace_id, issuer, audience, credential_id, credential_revision, record, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
`, result.ID, result.WorkspaceID, result.Issuer, result.Audience, result.Reference.ID, result.Reference.Revision, raw, now); err != nil {
			return opaqueIngressPostgresConflict(err)
		}
		if err := insertPostgresOpaqueIngressOperation(ctx, tx, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "credential_revocation", raw, now); err != nil {
			return err
		}
		return insertPostgresOpaqueIngressAudit(ctx, tx, OpaqueIngressAudit{
			ID: NewID("opaque-audit"), WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			SubjectKind: "credential_snapshot", SubjectID: result.Reference.ID + "/" + result.Reference.Revision,
			Kind: "revoked", Detail: result.Reason, OperationID: result.OperationID, Actor: result.Actor, CreatedAt: now,
		})
	})
	return result, replay, err
}

func (s *PostgresStore) PutOpaqueIngressPublicationRevision(ctx context.Context, request OpaqueIngressPublicationRevisionRequest) (OpaqueIngressPublicationRevision, bool, error) {
	request, err := normalizeOpaqueIngressPublicationRequest(request)
	if err != nil {
		return OpaqueIngressPublicationRevision{}, false, err
	}
	var result OpaqueIngressPublicationRevision
	var replay bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := postgresOpaqueIngressLock(ctx, tx, "publication", request.Revision.Issuer, request.Revision.Audience, request.Revision.PublicationRef, request.Revision.Revision); err != nil {
			return err
		}
		if existing, ok, err := postgresOpaqueIngressReplay[OpaqueIngressPublicationRevision](ctx, tx, request.Revision.WorkspaceID, request.OperationID, request.RequestFingerprint, "publication_revision"); err != nil {
			return err
		} else if ok {
			result, replay = existing, true
			return nil
		}
		// Publication validation and history pruning must be linearized. Without
		// this shared workspace lock, retention can delete a credential after it
		// was validated but before the immutable publication record is inserted.
		if err := postgresOpaqueIngressLock(ctx, tx, "retention", request.Revision.WorkspaceID); err != nil {
			return err
		}
		if err := requirePostgresOpaqueIngressWorkspace(ctx, tx, request.Revision.WorkspaceID); err != nil {
			return err
		}
		for _, reference := range request.Revision.CredentialRefs {
			credential, revoked, err := postgresOpaqueIngressCredential(ctx, tx, request.Revision.WorkspaceID, request.Revision.Issuer, request.Revision.Audience, reference.ID, reference.Revision)
			if err != nil || revoked || !opaqueIngressCredentialRefEqual(credential.Reference, reference) ||
				credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) ||
				credential.OperationRef != request.Revision.OperationRef ||
				validateOpaqueIngressCombinedReferences(request.Revision.References, credential.References) != nil {
				return fmt.Errorf("%w: credential snapshot is missing, revoked, or mixed", ErrOpaqueIngressProjectionRejected)
			}
		}
		result = cloneOpaqueIngressPublication(request.Revision)
		result.CreatedAt = time.Now().UTC()
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_publication_revision (
    workspace_id, issuer, audience, publication_ref, revision, digest, app_key, action_key, record, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, result.WorkspaceID, result.Issuer, result.Audience, result.PublicationRef, result.Revision, result.Digest, result.App, result.Action, raw, result.CreatedAt); err != nil {
			return opaqueIngressPostgresConflict(err)
		}
		if err := insertPostgresOpaqueIngressOperation(ctx, tx, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "publication_revision", raw, result.CreatedAt); err != nil {
			return err
		}
		return insertPostgresOpaqueIngressAudit(ctx, tx, OpaqueIngressAudit{
			ID: NewID("opaque-audit"), WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			PublicationRef: result.PublicationRef, SubjectKind: "publication_revision", SubjectID: result.Revision,
			Kind: "created", OperationID: result.OperationID, Actor: result.Actor, CreatedAt: result.CreatedAt,
		})
	})
	return cloneOpaqueIngressPublication(result), replay, err
}

func (s *PostgresStore) ActivateOpaqueIngressPublication(ctx context.Context, request OpaqueIngressActivationRequest) (OpaqueIngressActivation, bool, error) {
	request, err := normalizeOpaqueIngressActivationRequest(request)
	if err != nil {
		return OpaqueIngressActivation{}, false, err
	}
	var result OpaqueIngressActivation
	var replay bool
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := postgresOpaqueIngressLock(ctx, tx, "head", request.Issuer, request.Audience, request.PublicationRef); err != nil {
			return err
		}
		if existing, ok, err := postgresOpaqueIngressReplay[OpaqueIngressActivation](ctx, tx, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "activation"); err != nil {
			return err
		} else if ok {
			if existing.AuthorizedTarget != request.AuthorizedTarget {
				return fmt.Errorf("%w: operation authorization target changed", ErrConflict)
			}
			result, replay = existing, true
			return nil
		}
		var head OpaqueIngressProjectionHead
		err := tx.QueryRow(ctx, `
SELECT workspace_id, issuer, audience, publication_ref, generation, revision, publication_digest, state, updated_by, updated_at
FROM opaque_ingress_head
WHERE issuer=$1 AND audience=$2 AND publication_ref=$3
FOR UPDATE
`, request.Issuer, request.Audience, request.PublicationRef).Scan(
			&head.WorkspaceID, &head.Issuer, &head.Audience, &head.PublicationRef, &head.Generation,
			&head.Revision, &head.PublicationDigest, &head.State, &head.UpdatedBy, &head.UpdatedAt,
		)
		hasHead := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if hasHead && head.WorkspaceID != request.WorkspaceID {
			return ErrConflict
		}
		currentGeneration := int64(0)
		if hasHead {
			currentGeneration = head.Generation
		}
		if currentGeneration != request.ExpectedGeneration {
			return fmt.Errorf("%w: expected generation %d, current generation %d", ErrConflict, request.ExpectedGeneration, currentGeneration)
		}
		stateValue := OpaqueIngressActivationActive
		targetRevision := request.TargetRevision
		if request.Kind == OpaqueIngressActivationKindRevoke {
			if !hasHead {
				return ErrInvalidState
			}
			targetRevision = head.Revision
			stateValue = OpaqueIngressActivationRevoked
		} else if request.Kind == OpaqueIngressActivationKindRollback && !hasHead {
			return ErrInvalidState
		}
		var rawPublication []byte
		if err := tx.QueryRow(ctx, `
SELECT record FROM opaque_ingress_publication_revision
WHERE workspace_id=$1 AND issuer=$2 AND audience=$3 AND publication_ref=$4 AND revision=$5
FOR SHARE
`, request.WorkspaceID, request.Issuer, request.Audience, request.PublicationRef, targetRevision).Scan(&rawPublication); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var publication OpaqueIngressPublicationRevision
		if err := json.Unmarshal(rawPublication, &publication); err != nil {
			return err
		}
		if publication.Digest != OpaqueIngressPublicationRevisionDigest(publication) {
			return ErrOpaqueIngressProjectionRejected
		}
		if request.AuthorizedTarget != "" && request.AuthorizedTarget != publication.App+"/"+publication.Action {
			return fmt.Errorf("%w: activation target is not authorized", ErrOpaqueIngressProjectionRejected)
		}
		now := time.Now().UTC()
		if stateValue == OpaqueIngressActivationActive {
			if err := validatePostgresOpaqueIngressPublication(ctx, tx, publication, now); err != nil {
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
		rawActivation, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_activation (
    workspace_id, issuer, audience, publication_ref, generation, revision,
    publication_digest, state, kind, record, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
`, result.WorkspaceID, result.Issuer, result.Audience, result.PublicationRef, result.Generation,
			result.Revision, result.PublicationDigest, result.State, result.Kind, rawActivation, result.CreatedAt); err != nil {
			return opaqueIngressPostgresConflict(err)
		}
		if hasHead {
			command, err := tx.Exec(ctx, `
UPDATE opaque_ingress_head
SET generation=$5, revision=$6, publication_digest=$7, state=$8, updated_by=$9, updated_at=$10
WHERE workspace_id=$1 AND issuer=$2 AND audience=$3 AND publication_ref=$4 AND generation=$11
`, result.WorkspaceID, result.Issuer, result.Audience, result.PublicationRef, result.Generation,
				result.Revision, result.PublicationDigest, result.State, result.Actor, now, request.ExpectedGeneration)
			if err != nil {
				return err
			}
			if command.RowsAffected() != 1 {
				return ErrConflict
			}
		} else {
			if _, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_head (
    workspace_id, issuer, audience, publication_ref, generation, revision,
    publication_digest, state, updated_by, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
`, result.WorkspaceID, result.Issuer, result.Audience, result.PublicationRef, result.Generation,
				result.Revision, result.PublicationDigest, result.State, result.Actor, now); err != nil {
				return opaqueIngressPostgresConflict(err)
			}
		}
		if err := insertPostgresOpaqueIngressOperation(ctx, tx, result.WorkspaceID, result.OperationID, result.RequestFingerprint, "activation", rawActivation, now); err != nil {
			return err
		}
		return insertPostgresOpaqueIngressAudit(ctx, tx, OpaqueIngressAudit{
			ID: NewID("opaque-audit"), WorkspaceID: result.WorkspaceID, Issuer: result.Issuer, Audience: result.Audience,
			PublicationRef: result.PublicationRef, Generation: result.Generation,
			SubjectKind: "publication", SubjectID: result.Revision, Kind: result.Kind,
			OperationID: result.OperationID, Actor: result.Actor, CreatedAt: now,
		})
	})
	return result, replay, err
}

func (s *PostgresStore) ResolveOpaqueIngressProjection(ctx context.Context, request OpaqueIngressResolutionRequest) (OpaqueIngressResolvedProjection, error) {
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
	var rawActivation, rawPublication, rawCredential, rawDeployment []byte
	var headWorkspace, headRevision, headDigest string
	var activationDigest, publicationDigest, credentialDigest string
	var revokedID *string
	err := s.pool.QueryRow(ctx, `
SELECT head.workspace_id, head.revision, head.publication_digest,
       activation.publication_digest, publication.digest, credential.digest,
       activation.record, publication.record, credential.record, revocation.id, release.deployment
FROM opaque_ingress_head head
JOIN opaque_ingress_activation activation
  ON activation.workspace_id=head.workspace_id AND activation.issuer=head.issuer
 AND activation.audience=head.audience AND activation.publication_ref=head.publication_ref
 AND activation.generation=head.generation AND activation.revision=head.revision
 AND activation.publication_digest=head.publication_digest
JOIN opaque_ingress_publication_revision publication
  ON publication.workspace_id=head.workspace_id AND publication.issuer=head.issuer
 AND publication.audience=head.audience AND publication.publication_ref=head.publication_ref
 AND publication.revision=head.revision AND publication.digest=head.publication_digest
JOIN opaque_ingress_credential_snapshot credential
  ON credential.workspace_id=head.workspace_id AND credential.issuer=head.issuer
 AND credential.audience=head.audience AND credential.credential_id=$5
 AND credential.credential_revision=$6
LEFT JOIN opaque_ingress_credential_revocation revocation
  ON revocation.workspace_id=credential.workspace_id AND revocation.issuer=credential.issuer
 AND revocation.audience=credential.audience AND revocation.credential_id=credential.credential_id
 AND revocation.credential_revision=credential.credential_revision
JOIN control_active_release release
  ON release.workspace_id=publication.workspace_id AND release.app_key=publication.app_key
WHERE head.issuer=$1 AND head.audience=$2 AND head.publication_ref=$3
  AND head.generation=$4 AND head.state='active' AND activation.state='active'
`, request.Issuer, request.Audience, request.PublicationRef, request.RouteGeneration,
		request.CredentialID, request.CredentialRevision).Scan(
		&headWorkspace, &headRevision, &headDigest, &activationDigest, &publicationDigest, &credentialDigest,
		&rawActivation, &rawPublication, &rawCredential, &revokedID, &rawDeployment,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if revokedID != nil {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	var result OpaqueIngressResolvedProjection
	var deployment contract.Deployment
	if err := json.Unmarshal(rawActivation, &result.Activation); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := json.Unmarshal(rawPublication, &result.Publication); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := json.Unmarshal(rawCredential, &result.Credential); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := json.Unmarshal(rawDeployment, &deployment); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if headWorkspace != result.Publication.WorkspaceID || headWorkspace != result.Credential.WorkspaceID ||
		headRevision != result.Publication.Revision || headDigest != result.Publication.Digest ||
		activationDigest != result.Activation.PublicationDigest || publicationDigest != result.Publication.Digest ||
		credentialDigest != result.Credential.Reference.Digest ||
		result.Activation.WorkspaceID != headWorkspace || result.Activation.Issuer != request.Issuer ||
		result.Activation.Audience != request.Audience || result.Activation.PublicationRef != request.PublicationRef ||
		result.Activation.Generation != request.RouteGeneration || result.Activation.State != OpaqueIngressActivationActive ||
		result.Activation.Revision != result.Publication.Revision || result.Activation.PublicationDigest != result.Publication.Digest ||
		result.Publication.Issuer != request.Issuer || result.Publication.Audience != request.Audience ||
		result.Publication.PublicationRef != request.PublicationRef ||
		result.Credential.Issuer != request.Issuer || result.Credential.Audience != request.Audience ||
		result.Credential.Reference.ID != request.CredentialID || result.Credential.Reference.Revision != request.CredentialRevision ||
		result.Publication.Digest != OpaqueIngressPublicationRevisionDigest(result.Publication) ||
		result.Credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(result.Credential) {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if result.Publication.HTTP.Method != request.Method || result.Publication.HTTP.ExactEscapedPath != request.ExactEscapedPath || result.Publication.HTTP.ContentType != request.ContentType || request.BodyByteLength > result.Publication.HTTP.MaxRequestBodyBytes {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if !opaqueIngressPublicationHasCredential(result.Publication, result.Credential.Reference) || result.Publication.OperationRef != result.Credential.OperationRef || validateOpaqueIngressCombinedReferences(result.Publication.References, result.Credential.References) != nil || !opaqueIngressReleaseMatches(result.Publication.Release, deployment.DeploymentID, deployment.Commit, deployment.BundleDigest) {
		return OpaqueIngressResolvedProjection{}, ErrOpaqueIngressProjectionRejected
	}
	if err := validateOpaqueIngressFreshAt(result.Publication.ProjectedAt, result.Publication.NotAfter, result.Publication.MaxStalenessSeconds, request.Now); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	if err := validateOpaqueIngressFreshAt(result.Credential.ProjectedAt, result.Credential.NotAfter, result.Credential.MaxStalenessSeconds, request.Now); err != nil {
		return OpaqueIngressResolvedProjection{}, err
	}
	result.Publication = cloneOpaqueIngressPublication(result.Publication)
	result.Credential = cloneOpaqueIngressCredential(result.Credential)
	return result, nil
}

func (s *PostgresStore) ListOpaqueIngressProjectionAudit(ctx context.Context, workspaceID string, publicationRef string, limit int) ([]OpaqueIngressAudit, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	publicationRef = strings.TrimSpace(publicationRef)
	if limit <= 0 || limit > 1000 || (publicationRef != "" && (len(publicationRef) > 100 || !opaqueIngressPublicationRefPattern.MatchString(publicationRef))) {
		return nil, fmt.Errorf("%w: audit limit must be between 1 and 1000", ErrInvalidState)
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, workspace_id, issuer, audience, publication_ref, generation, subject_kind,
       subject_id, kind, detail, operation_id, actor, created_at
FROM opaque_ingress_audit
WHERE workspace_id=$1 AND ($2='' OR publication_ref=$2)
ORDER BY created_at DESC, id DESC
LIMIT $3
`, workspaceID, publicationRef, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]OpaqueIngressAudit, 0)
	for rows.Next() {
		var record OpaqueIngressAudit
		if err := rows.Scan(&record.ID, &record.WorkspaceID, &record.Issuer, &record.Audience,
			&record.PublicationRef, &record.Generation, &record.SubjectKind, &record.SubjectID,
			&record.Kind, &record.Detail, &record.OperationID, &record.Actor, &record.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}

func (s *PostgresStore) PruneOpaqueIngressProjectionHistory(ctx context.Context, request OpaqueIngressRetentionRequest) (OpaqueIngressRetentionResult, bool, error) {
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
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if err := postgresOpaqueIngressLock(ctx, tx, "retention", request.WorkspaceID); err != nil {
			return err
		}
		if existing, ok, err := postgresOpaqueIngressReplay[OpaqueIngressRetentionResult](ctx, tx, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "retention"); err != nil {
			return err
		} else if ok {
			result = existing
			replay = true
			return nil
		}
		if err := requirePostgresOpaqueIngressWorkspace(ctx, tx, request.WorkspaceID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
SELECT issuer, audience, publication_ref, revision, record
FROM opaque_ingress_publication_revision publication
WHERE workspace_id=$1 AND created_at<$2
  AND NOT EXISTS (
      SELECT 1 FROM opaque_ingress_activation activation
      WHERE activation.workspace_id=publication.workspace_id AND activation.issuer=publication.issuer
        AND activation.audience=publication.audience AND activation.publication_ref=publication.publication_ref
        AND activation.revision=publication.revision
  )
ORDER BY created_at, issuer, audience, publication_ref, revision
FOR UPDATE
`, request.WorkspaceID, request.Before)
		if err != nil {
			return err
		}
		type publicationCandidate struct{ issuer, audience, publicationRef, revision string }
		candidates := make([]publicationCandidate, 0)
		now := time.Now().UTC()
		for rows.Next() && len(candidates) < request.Limit {
			var candidate publicationCandidate
			var raw []byte
			if err := rows.Scan(&candidate.issuer, &candidate.audience, &candidate.publicationRef, &candidate.revision, &raw); err != nil {
				rows.Close()
				return err
			}
			var publication OpaqueIngressPublicationRevision
			if err := json.Unmarshal(raw, &publication); err != nil {
				rows.Close()
				return err
			}
			if !publication.NotAfter.After(request.Before) && !publication.RetainUntil.After(request.Before) {
				candidates = append(candidates, candidate)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, candidate := range candidates {
			command, err := tx.Exec(ctx, `
DELETE FROM opaque_ingress_publication_revision publication
WHERE workspace_id=$5 AND issuer=$1 AND audience=$2 AND publication_ref=$3 AND revision=$4
  AND NOT EXISTS (
      SELECT 1 FROM opaque_ingress_activation activation
      WHERE activation.workspace_id=publication.workspace_id AND activation.issuer=publication.issuer
        AND activation.audience=publication.audience AND activation.publication_ref=publication.publication_ref
        AND activation.revision=publication.revision
  )
`, candidate.issuer, candidate.audience, candidate.publicationRef, candidate.revision, request.WorkspaceID)
			if err != nil {
				return err
			}
			result.PublicationRevisions += command.RowsAffected()
		}
		remaining := int64(request.Limit) - result.PublicationRevisions
		if remaining > 0 {
			command, err := tx.Exec(ctx, `
DELETE FROM opaque_ingress_credential_snapshot credential
WHERE ctid IN (
    SELECT credential.ctid
    FROM opaque_ingress_credential_snapshot credential
    WHERE credential.workspace_id=$1 AND credential.created_at<$2
	  AND (credential.record->>'notAfter')::timestamptz <= $2
	  AND NOT EXISTS (
	      SELECT 1 FROM opaque_ingress_credential_revocation revocation
	      WHERE revocation.workspace_id=credential.workspace_id AND revocation.issuer=credential.issuer
	        AND revocation.audience=credential.audience AND revocation.credential_id=credential.credential_id
	        AND revocation.credential_revision=credential.credential_revision
	  )
      AND NOT EXISTS (
          SELECT 1 FROM opaque_ingress_publication_revision publication
          WHERE publication.workspace_id=credential.workspace_id
            AND publication.issuer=credential.issuer AND publication.audience=credential.audience
            AND publication.record->'credentialRefs' @> jsonb_build_array(jsonb_build_object(
                'id', credential.credential_id,
                'revision', credential.credential_revision,
                'digest', credential.digest
            ))
      )
    ORDER BY credential.created_at, credential.issuer, credential.audience,
             credential.credential_id, credential.credential_revision
    LIMIT $3
    FOR UPDATE
)
			`, request.WorkspaceID, request.Before, remaining)
			if err != nil {
				return err
			}
			result.CredentialSnapshots = command.RowsAffected()
		}
		now = time.Now().UTC()
		rawResult, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if err := insertPostgresOpaqueIngressOperation(ctx, tx, request.WorkspaceID, request.OperationID, request.RequestFingerprint, "retention", rawResult, now); err != nil {
			return err
		}
		return insertPostgresOpaqueIngressAudit(ctx, tx, OpaqueIngressAudit{
			ID: NewID("opaque-audit"), WorkspaceID: request.WorkspaceID,
			SubjectKind: "retention", SubjectID: request.WorkspaceID, Kind: "pruned",
			Detail:      fmt.Sprintf("publication_revisions=%d credential_snapshots=%d", result.PublicationRevisions, result.CredentialSnapshots),
			OperationID: request.OperationID, Actor: request.Actor, CreatedAt: now,
		})
	})
	return result, replay, err
}

func validatePostgresOpaqueIngressPublication(ctx context.Context, tx pgx.Tx, publication OpaqueIngressPublicationRevision, now time.Time) error {
	if err := validateOpaqueIngressFreshAt(publication.ProjectedAt, publication.NotAfter, publication.MaxStalenessSeconds, now); err != nil {
		return err
	}
	var rawDeployment []byte
	if err := tx.QueryRow(ctx, `
SELECT deployment FROM control_active_release
WHERE workspace_id=$1 AND app_key=$2
FOR SHARE
`, publication.WorkspaceID, publication.App).Scan(&rawDeployment); errors.Is(err, pgx.ErrNoRows) {
		return ErrOpaqueIngressProjectionRejected
	} else if err != nil {
		return err
	}
	var deployment contract.Deployment
	if err := json.Unmarshal(rawDeployment, &deployment); err != nil {
		return err
	}
	if !opaqueIngressReleaseMatches(publication.Release, deployment.DeploymentID, deployment.Commit, deployment.BundleDigest) {
		return ErrOpaqueIngressProjectionRejected
	}
	for _, reference := range publication.CredentialRefs {
		credential, revoked, err := postgresOpaqueIngressCredential(ctx, tx, publication.WorkspaceID, publication.Issuer, publication.Audience, reference.ID, reference.Revision)
		if err != nil || revoked || !opaqueIngressCredentialRefEqual(credential.Reference, reference) || credential.Reference.Digest != OpaqueIngressCredentialSnapshotDigest(credential) || credential.OperationRef != publication.OperationRef || validateOpaqueIngressCombinedReferences(publication.References, credential.References) != nil {
			return ErrOpaqueIngressProjectionRejected
		}
		if err := validateOpaqueIngressFreshAt(credential.ProjectedAt, credential.NotAfter, credential.MaxStalenessSeconds, now); err != nil {
			return err
		}
	}
	return nil
}

func postgresOpaqueIngressCredential(ctx context.Context, tx pgx.Tx, workspaceID, issuer, audience, id, revision string) (OpaqueIngressCredentialSnapshot, bool, error) {
	var raw []byte
	var revoked bool
	err := tx.QueryRow(ctx, `
SELECT credential.record, EXISTS (
    SELECT 1 FROM opaque_ingress_credential_revocation revocation
    WHERE revocation.workspace_id=credential.workspace_id AND revocation.issuer=credential.issuer
      AND revocation.audience=credential.audience AND revocation.credential_id=credential.credential_id
      AND revocation.credential_revision=credential.credential_revision
)
FROM opaque_ingress_credential_snapshot credential
WHERE credential.workspace_id=$1 AND credential.issuer=$2 AND credential.audience=$3
  AND credential.credential_id=$4 AND credential.credential_revision=$5
FOR SHARE
`, workspaceID, issuer, audience, id, revision).Scan(&raw, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return OpaqueIngressCredentialSnapshot{}, false, ErrNotFound
	}
	if err != nil {
		return OpaqueIngressCredentialSnapshot{}, false, err
	}
	var result OpaqueIngressCredentialSnapshot
	if err := json.Unmarshal(raw, &result); err != nil {
		return OpaqueIngressCredentialSnapshot{}, false, err
	}
	return cloneOpaqueIngressCredential(result), revoked, nil
}

func opaqueIngressPublicationHasCredential(publication OpaqueIngressPublicationRevision, reference OpaqueIngressCredentialSnapshotRef) bool {
	for _, candidate := range publication.CredentialRefs {
		if opaqueIngressCredentialRefEqual(candidate, reference) {
			return true
		}
	}
	return false
}

func postgresOpaqueIngressLock(ctx context.Context, tx pgx.Tx, parts ...string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, strings.Join(parts, "\x1f"))
	return err
}

func requirePostgresOpaqueIngressWorkspace(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace_registry WHERE id=$1)`, workspaceID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func postgresOpaqueIngressReplay[T any](ctx context.Context, tx pgx.Tx, workspaceID, operationID, fingerprint, kind string) (T, bool, error) {
	var zero T
	var existingFingerprint, existingKind string
	var raw []byte
	err := tx.QueryRow(ctx, `
SELECT request_fingerprint, kind, result
FROM opaque_ingress_operation
WHERE workspace_id=$1 AND operation_id=$2
`, workspaceID, operationID).Scan(&existingFingerprint, &existingKind, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if existingFingerprint != fingerprint || existingKind != kind {
		return zero, false, fmt.Errorf("%w: operation ID was reused with another request", ErrConflict)
	}
	if err := json.Unmarshal(raw, &zero); err != nil {
		return zero, false, err
	}
	return zero, true, nil
}

func insertPostgresOpaqueIngressOperation(ctx context.Context, tx pgx.Tx, workspaceID, operationID, fingerprint, kind string, result []byte, now time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_operation (
    workspace_id, operation_id, request_fingerprint, kind, result, created_at
) VALUES ($1,$2,$3,$4,$5,$6)
`, workspaceID, operationID, fingerprint, kind, result, now)
	return opaqueIngressPostgresConflict(err)
}

func insertPostgresOpaqueIngressAudit(ctx context.Context, tx pgx.Tx, record OpaqueIngressAudit) error {
	_, err := tx.Exec(ctx, `
INSERT INTO opaque_ingress_audit (
    id, workspace_id, issuer, audience, publication_ref, generation, subject_kind,
    subject_id, kind, detail, operation_id, actor, created_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
`, record.ID, record.WorkspaceID, record.Issuer, record.Audience, record.PublicationRef,
		record.Generation, record.SubjectKind, record.SubjectID, record.Kind, record.Detail,
		record.OperationID, record.Actor, record.CreatedAt)
	return err
}

func opaqueIngressPostgresConflict(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503") {
		return fmt.Errorf("%w: %s", ErrConflict, pgErr.ConstraintName)
	}
	return err
}
