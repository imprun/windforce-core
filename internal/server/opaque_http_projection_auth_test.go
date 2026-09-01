package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	executionpkg "github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

func TestOpaqueHTTPProjectionRequestScope(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		wantScope executionpkg.Scope
		want      bool
	}{
		{name: "collection read", method: http.MethodGet, path: "/api/w/ws-a/opaque-http-projections", wantScope: executionpkg.ScopeOpaqueHTTPProjectionsRead, want: true},
		{name: "nested head", method: http.MethodHead, path: "/api/w/ws-a/opaque-http-projections/publication-a", wantScope: executionpkg.ScopeOpaqueHTTPProjectionsRead, want: true},
		{name: "create", method: http.MethodPost, path: "/api/w/ws-a/opaque-http-projections", wantScope: executionpkg.ScopeOpaqueHTTPProjectionsWrite, want: true},
		{name: "update", method: http.MethodPut, path: "/api/w/ws-a/opaque-http-projections/publication-a/active", wantScope: executionpkg.ScopeOpaqueHTTPProjectionsWrite, want: true},
		{name: "options is not a read", method: http.MethodOptions, path: "/api/w/ws-a/opaque-http-projections", wantScope: executionpkg.ScopeOpaqueHTTPProjectionsWrite, want: true},
		{name: "lookalike resource", method: http.MethodGet, path: "/api/w/ws-a/opaque-http-projections-preview", want: false},
		{name: "public plane", method: http.MethodGet, path: "/api/v1/opaque-http-projections", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://example.test"+test.path, nil)
			gotScope, got := opaqueHTTPProjectionRequestScope(request)
			if got != test.want || gotScope != test.wantScope {
				t.Fatalf("opaqueHTTPProjectionRequestScope() = (%q, %t), want (%q, %t)", gotScope, got, test.wantScope, test.want)
			}
		})
	}
}

func TestOpaqueHTTPProjectionAuthorizationRequiresExplicitAdminOrServicePrincipal(t *testing.T) {
	ctx := context.Background()
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	if _, err := store.CreateWorkspace(ctx, "ws-a", "Workspace A", "seed"); err != nil {
		t.Fatalf("CreateWorkspace returned error: %v", err)
	}

	const (
		workspaceToken = "wfw_projection_workspace"
		readToken      = "wfs_projection_read"
		writeToken     = "wfs_projection_write"
		bothToken      = "wfs_projection_read_write"
		unrelatedToken = "wfs_projection_unrelated"
	)
	if _, err := store.CreateWorkspaceToken(ctx, "ws-a", "Workspace automation", state.HashWorkspaceToken(workspaceToken), "seed"); err != nil {
		t.Fatalf("CreateWorkspaceToken returned error: %v", err)
	}
	readPrincipal, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Projection observer", Scopes: []string{string(executionpkg.ScopeOpaqueHTTPProjectionsRead)},
	}, state.HashBearerToken(readToken), "seed")
	if err != nil {
		t.Fatalf("CreateServicePrincipal read returned error: %v", err)
	}
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Projection writer", Scopes: []string{string(executionpkg.ScopeOpaqueHTTPProjectionsWrite)},
	}, state.HashBearerToken(writeToken), "seed"); err != nil {
		t.Fatalf("CreateServicePrincipal write returned error: %v", err)
	}
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Projection reconciler", Scopes: []string{
			string(executionpkg.ScopeOpaqueHTTPProjectionsRead),
			string(executionpkg.ScopeOpaqueHTTPProjectionsWrite),
		},
	}, state.HashBearerToken(bothToken), "seed"); err != nil {
		t.Fatalf("CreateServicePrincipal read-write returned error: %v", err)
	}
	if _, err := store.CreateServicePrincipal(ctx, state.ServicePrincipal{
		WorkspaceID: "ws-a", Name: "Run submitter", Scopes: []string{string(executionpkg.ScopeRunsCreate)},
	}, state.HashBearerToken(unrelatedToken), "seed"); err != nil {
		t.Fatalf("CreateServicePrincipal unrelated returned error: %v", err)
	}

	tests := []struct {
		name       string
		adminToken string
		method     string
		token      string
		wantStatus int
		wantActor  string
	}{
		{name: "anonymous development mode is denied", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
		{name: "unconfigured admin-like token is denied", method: http.MethodGet, token: "admin-token", wantStatus: http.StatusUnauthorized},
		{name: "missing configured admin token is denied", adminToken: "admin-token", method: http.MethodGet, wantStatus: http.StatusUnauthorized},
		{name: "explicit admin may read", adminToken: "admin-token", method: http.MethodGet, token: "admin-token"},
		{name: "explicit admin may mutate", adminToken: "admin-token", method: http.MethodPost, token: "admin-token"},
		{name: "workspace token is forbidden", adminToken: "admin-token", method: http.MethodGet, token: workspaceToken, wantStatus: http.StatusForbidden},
		{name: "read principal may read", adminToken: "admin-token", method: http.MethodGet, token: readToken, wantActor: "service:" + readPrincipal.ID},
		{name: "read principal may head", adminToken: "admin-token", method: http.MethodHead, token: readToken, wantActor: "service:" + readPrincipal.ID},
		{name: "read principal may not mutate", adminToken: "admin-token", method: http.MethodPost, token: readToken, wantStatus: http.StatusForbidden},
		{name: "write principal may mutate", adminToken: "admin-token", method: http.MethodPut, token: writeToken},
		{name: "write principal may not read", adminToken: "admin-token", method: http.MethodGet, token: writeToken, wantStatus: http.StatusForbidden},
		{name: "read-write principal may revoke", adminToken: "admin-token", method: http.MethodDelete, token: bothToken},
		{name: "unrelated principal is forbidden", adminToken: "admin-token", method: http.MethodGet, token: unrelatedToken, wantStatus: http.StatusForbidden},
		{name: "unknown token is unauthorized", adminToken: "admin-token", method: http.MethodGet, token: "wfs_unknown", wantStatus: http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "http://example.test/api/w/ws-a/opaque-http-projections/publication-a", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			request.Header.Set("X-Windforce-Actor", "spoofed-actor")
			handler := &Handler{store: store, adminToken: test.adminToken}
			authorizedRequest, status, message := handler.authorizeAPIRequest(request)
			if status != test.wantStatus {
				t.Fatalf("authorizeAPIRequest status = %d, want %d: %s", status, test.wantStatus, message)
			}
			if status == 0 && test.wantActor != "" {
				if got := requestActorSubject(authorizedRequest); got != test.wantActor {
					t.Fatalf("request actor = %q, want %q", got, test.wantActor)
				}
			}
		})
	}
}
