package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestManagedWorkspaceAPIAndAuthorizationBoundary(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	server := httptest.NewServer(New(Config{
		Store: store, ManagedWorkspaces: true, AdminToken: "instance-admin",
	}))
	defer server.Close()

	create := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces", "instance-admin", `{"id":"team-a","name":"Team A"}`)
	if create.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d: %s", create.StatusCode, readResponse(t, create))
	}
	var created workspaceView
	decodeResponse(t, create, &created)
	if created.ID != "team-a" {
		t.Fatalf("create response = %#v", created)
	}
	issue := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/team-a/tokens", "instance-admin", `{"name":"CLI"}`)
	if issue.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d: %s", issue.StatusCode, readResponse(t, issue))
	}
	var issued struct {
		Token    workspaceTokenView `json:"token"`
		APIToken string             `json:"api_token"`
	}
	decodeResponse(t, issue, &issued)
	if issued.Token.ID == "" || issued.Token.Name != "CLI" || issued.APIToken == "" {
		t.Fatalf("issue response = %#v", issued)
	}

	unknown := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/typo/apps", "instance-admin", "")
	if unknown.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace status = %d: %s", unknown.StatusCode, readResponse(t, unknown))
	}

	scoped := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", issued.APIToken, "")
	if scoped.StatusCode != http.StatusOK {
		t.Fatalf("workspace token status = %d: %s", scoped.StatusCode, readResponse(t, scoped))
	}
	global := workspaceRequest(t, server.URL, http.MethodGet, "/api/workspaces", issued.APIToken, "")
	if global.StatusCode != http.StatusUnauthorized {
		t.Fatalf("workspace token global status = %d: %s", global.StatusCode, readResponse(t, global))
	}

	createB := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces", "instance-admin", `{"id":"team-b","name":"Team B"}`)
	if createB.StatusCode != http.StatusCreated {
		t.Fatalf("create B status = %d: %s", createB.StatusCode, readResponse(t, createB))
	}
	issueB := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/team-b/tokens", "instance-admin", `{"name":"CLI"}`)
	var issuedB struct {
		APIToken string `json:"api_token"`
	}
	decodeResponse(t, issueB, &issuedB)
	cross := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", issuedB.APIToken, "")
	if cross.StatusCode != http.StatusUnauthorized {
		t.Fatalf("cross-workspace status = %d: %s", cross.StatusCode, readResponse(t, cross))
	}

	rotate := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/team-a/tokens/"+issued.Token.ID+"/rotate", "instance-admin", "")
	if rotate.StatusCode != http.StatusOK {
		t.Fatalf("rotate status = %d: %s", rotate.StatusCode, readResponse(t, rotate))
	}
	var rotated struct {
		APIToken string `json:"api_token"`
	}
	decodeResponse(t, rotate, &rotated)
	oldToken := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", issued.APIToken, "")
	if oldToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old token status = %d: %s", oldToken.StatusCode, readResponse(t, oldToken))
	}
	newToken := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", rotated.APIToken, "")
	if newToken.StatusCode != http.StatusOK {
		t.Fatalf("new token status = %d: %s", newToken.StatusCode, readResponse(t, newToken))
	}
	client := workspaceRequest(t, server.URL, http.MethodPost, "/api/w/team-a/clients", "instance-admin", `{"name":"Archived client"}`)
	if client.StatusCode != http.StatusCreated {
		t.Fatalf("client create status = %d: %s", client.StatusCode, readResponse(t, client))
	}
	var issuedClient struct {
		Client clientView `json:"client"`
	}
	decodeResponse(t, client, &issuedClient)
	clientPath := "/api/w/team-a/clients/" + issuedClient.Client.ID
	servicePrincipal := workspaceRequest(t, server.URL, http.MethodPost, "/api/w/team-a/service-principals", "instance-admin", `{"name":"Archived service","scopes":["runs:create"]}`)
	if servicePrincipal.StatusCode != http.StatusCreated {
		t.Fatalf("service principal create status = %d: %s", servicePrincipal.StatusCode, readResponse(t, servicePrincipal))
	}
	var issuedServicePrincipal struct {
		ServicePrincipal servicePrincipalView `json:"service_principal"`
	}
	decodeResponse(t, servicePrincipal, &issuedServicePrincipal)
	servicePrincipalPath := "/api/w/team-a/service-principals/" + issuedServicePrincipal.ServicePrincipal.ID

	archive := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/team-a/archive", "instance-admin", "")
	if archive.StatusCode != http.StatusOK {
		t.Fatalf("archive status = %d: %s", archive.StatusCode, readResponse(t, archive))
	}
	readArchived := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", "instance-admin", "")
	if readArchived.StatusCode != http.StatusOK {
		t.Fatalf("archived read status = %d: %s", readArchived.StatusCode, readResponse(t, readArchived))
	}
	scopedReadArchived := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", rotated.APIToken, "")
	if scopedReadArchived.StatusCode != http.StatusOK {
		t.Fatalf("archived scoped read status = %d: %s", scopedReadArchived.StatusCode, readResponse(t, scopedReadArchived))
	}
	updateArchived := workspaceRequest(t, server.URL, http.MethodPatch, "/api/workspaces/team-a", "instance-admin", `{"name":"Archived Team"}`)
	if updateArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived update status = %d: %s", updateArchived.StatusCode, readResponse(t, updateArchived))
	}
	rotateArchived := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/team-a/tokens/"+issued.Token.ID+"/rotate", "instance-admin", "")
	if rotateArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived token rotation status = %d: %s", rotateArchived.StatusCode, readResponse(t, rotateArchived))
	}
	rotateClientArchived := workspaceRequest(t, server.URL, http.MethodPost, clientPath+"/token", "instance-admin", "")
	if rotateClientArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived client token rotation status = %d: %s", rotateClientArchived.StatusCode, readResponse(t, rotateClientArchived))
	}
	patchClientArchived := workspaceRequest(t, server.URL, http.MethodPatch, clientPath, "instance-admin", `{"name":"Changed"}`)
	if patchClientArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived client update status = %d: %s", patchClientArchived.StatusCode, readResponse(t, patchClientArchived))
	}
	revokeClientArchived := workspaceRequest(t, server.URL, http.MethodDelete, clientPath+"/token", "instance-admin", "")
	if revokeClientArchived.StatusCode != http.StatusOK {
		t.Fatalf("archived client token revoke status = %d: %s", revokeClientArchived.StatusCode, readResponse(t, revokeClientArchived))
	}
	var revokedClient clientView
	decodeResponse(t, revokeClientArchived, &revokedClient)
	if revokedClient.HasToken {
		t.Fatalf("archived client still has a token: %#v", revokedClient)
	}
	rotateServicePrincipalArchived := workspaceRequest(t, server.URL, http.MethodPost, servicePrincipalPath+"/token", "instance-admin", "")
	if rotateServicePrincipalArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived service principal token rotation status = %d: %s", rotateServicePrincipalArchived.StatusCode, readResponse(t, rotateServicePrincipalArchived))
	}
	revokeServicePrincipalArchived := workspaceRequest(t, server.URL, http.MethodDelete, servicePrincipalPath+"/token", "instance-admin", "")
	if revokeServicePrincipalArchived.StatusCode != http.StatusOK {
		t.Fatalf("archived service principal token revoke status = %d: %s", revokeServicePrincipalArchived.StatusCode, readResponse(t, revokeServicePrincipalArchived))
	}
	var revokedServicePrincipal servicePrincipalView
	decodeResponse(t, revokeServicePrincipalArchived, &revokedServicePrincipal)
	if revokedServicePrincipal.HasToken {
		t.Fatalf("archived service principal still has a token: %#v", revokedServicePrincipal)
	}
	writeArchived := workspaceRequest(t, server.URL, http.MethodPost, "/api/w/team-a/variables", "instance-admin", `{"path":"x","value":"y"}`)
	if writeArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived write status = %d: %s", writeArchived.StatusCode, readResponse(t, writeArchived))
	}
	executeArchived := workspaceRequest(t, server.URL, http.MethodPost, "/api/v1/workspaces/team-a/runs", "instance-admin", `{"app":"echo","action":"run","input":{}}`)
	if executeArchived.StatusCode != http.StatusConflict {
		t.Fatalf("archived execution status = %d: %s", executeArchived.StatusCode, readResponse(t, executeArchived))
	}

	revoke := workspaceRequest(t, server.URL, http.MethodDelete, "/api/workspaces/team-a/tokens/"+issued.Token.ID, "instance-admin", "")
	if revoke.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d: %s", revoke.StatusCode, readResponse(t, revoke))
	}
	revokedToken := workspaceRequest(t, server.URL, http.MethodGet, "/api/w/team-a/system/info", rotated.APIToken, "")
	if revokedToken.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d: %s", revokedToken.StatusCode, readResponse(t, revokedToken))
	}

	audit := workspaceRequest(t, server.URL, http.MethodGet, "/api/workspaces/team-a/audit", "instance-admin", "")
	if audit.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d: %s", audit.StatusCode, readResponse(t, audit))
	}
	var auditBody struct {
		Items []state.WorkspaceAudit `json:"items"`
	}
	decodeResponse(t, audit, &auditBody)
	if len(auditBody.Items) < 4 || auditBody.Items[0].Kind != "token_revoked" {
		t.Fatalf("audit response = %#v", auditBody)
	}
}

func TestManagedWorkspaceRejectsInvalidIDAndDefaultArchive(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	server := httptest.NewServer(New(Config{Store: store, ManagedWorkspaces: true}))
	defer server.Close()

	invalid := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces", "", `{"id":"Team A","name":"Team A"}`)
	if invalid.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d: %s", invalid.StatusCode, readResponse(t, invalid))
	}
	archive := workspaceRequest(t, server.URL, http.MethodPost, "/api/workspaces/default/archive", "", "")
	if archive.StatusCode != http.StatusConflict {
		t.Fatalf("archive default status = %d: %s", archive.StatusCode, readResponse(t, archive))
	}
}

func workspaceRequest(t *testing.T, baseURL string, method string, path string, token string, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, baseURL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Windforce-Actor", "test-admin")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWorkspaceManagementOpenAPI(t *testing.T) {
	paths := buildControlPlaneOpenAPI("http://example.test", "default")["paths"].(map[string]any)
	for _, path := range []string{
		"/api/workspaces",
		"/api/workspaces/{workspace_id}",
		"/api/workspaces/{workspace_id}/archive",
		"/api/workspaces/{workspace_id}/tokens",
		"/api/workspaces/{workspace_id}/tokens/{token_id}",
		"/api/workspaces/{workspace_id}/tokens/{token_id}/rotate",
		"/api/workspaces/{workspace_id}/audit",
	} {
		if _, ok := paths[path]; !ok {
			t.Errorf("OpenAPI path %q is missing", path)
		}
	}

	schemas := controlPlaneSchemas()
	for _, name := range []string{"Workspace", "WorkspaceListResponse", "CreateWorkspaceRequest", "UpdateWorkspaceRequest", "WorkspaceToken", "WorkspaceTokenResult", "WorkspaceTokenListResponse", "WorkspaceAudit"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("OpenAPI schema %q is missing", name)
		}
	}
}
