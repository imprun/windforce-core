package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

type workerPlanePrincipal struct {
	Credential *state.WorkerCredential
}

type workerPlanePrincipalContextKey struct{}

func workerPlanePrincipalFrom(ctx context.Context) *workerPlanePrincipal {
	principal, _ := ctx.Value(workerPlanePrincipalContextKey{}).(*workerPlanePrincipal)
	return principal
}

func (h *Handler) workerControlStore() (state.WorkerControlStore, bool) {
	store, ok := h.store.(state.WorkerControlStore)
	return store, ok
}

func (h *Handler) authenticateWorkerPlane(r *http.Request) (*http.Request, int, string) {
	value := bearer(r)
	if strings.HasPrefix(value, contract.RemoteWorkerTokenPrefix) {
		if store, ok := h.workerControlStore(); ok {
			credential, err := store.GetWorkerCredentialByTokenHash(r.Context(), state.HashBearerToken(value))
			if err == nil {
				principal := &workerPlanePrincipal{Credential: &credential}
				ctx := context.WithValue(r.Context(), workerPlanePrincipalContextKey{}, principal)
				return r.WithContext(ctx), 0, ""
			}
			if !errors.Is(err, state.ErrNotFound) {
				return r, http.StatusServiceUnavailable, "worker credential store unavailable"
			}
		}
	}
	legacyToken := h.workerToken
	if legacyToken == "" {
		legacyToken = h.adminToken
	}
	if !authorized(r, legacyToken) {
		return r, http.StatusUnauthorized, "unauthorized"
	}
	principal := &workerPlanePrincipal{}
	return r.WithContext(context.WithValue(r.Context(), workerPlanePrincipalContextKey{}, principal)), 0, ""
}

func managedWorkerCredential(r *http.Request) *state.WorkerCredential {
	principal := workerPlanePrincipalFrom(r.Context())
	if principal == nil {
		return nil
	}
	return principal.Credential
}

func managedWorkerRecord(ctx context.Context, store state.WorkerControlStore, credential state.WorkerCredential, workerID string) (state.WorkerRecord, error) {
	record, err := store.GetWorker(ctx, strings.TrimSpace(workerID))
	if err != nil {
		return state.WorkerRecord{}, err
	}
	if record.CredentialID != credential.ID || record.CredentialGeneration != credential.Generation || record.Group != credential.Group {
		return state.WorkerRecord{}, state.ErrForbidden
	}
	return record, nil
}

func credentialAllowsContinuation(credential state.WorkerCredential) bool {
	return credential.AllowsLeaseContinuation(time.Now().UTC())
}
