package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestAppRuntimeLifecycleControlAPI(t *testing.T) {
	store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
	server := httptest.NewServer(New(Config{Store: store}))
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/w/ws-a/apps/shop/runtime-lifecycle",
		bytes.NewBufferString(`{"state":"tombstoned","reason":"retiring","expectedRevision":0}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Windforce-Actor", "operator@example.test")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", response.StatusCode)
	}
	var lifecycle state.AppRuntimeLifecycle
	if json.NewDecoder(response.Body).Decode(&lifecycle) != nil || lifecycle.State != state.AppRuntimeTombstoned || lifecycle.Revision != 1 {
		t.Fatalf("lifecycle = %#v", lifecycle)
	}

	auditResponse, err := http.Get(server.URL + "/api/w/ws-a/apps/shop/runtime-lifecycle/audit")
	if err != nil {
		t.Fatal(err)
	}
	defer auditResponse.Body.Close()
	if auditResponse.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d", auditResponse.StatusCode)
	}

	forceRequest, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/w/ws-a/apps/shop/runtime-config?force=true", nil)
	forceResponse, err := http.DefaultClient.Do(forceRequest)
	if err != nil {
		t.Fatal(err)
	}
	forceResponse.Body.Close()
	if forceResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("unconfirmed force purge status = %d", forceResponse.StatusCode)
	}
	forceRequest, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/w/ws-a/apps/shop/runtime-config?force=true", nil)
	forceRequest.Header.Set("X-Windforce-Confirm-Force-Purge", "shop")
	forceResponse, err = http.DefaultClient.Do(forceRequest)
	if err != nil {
		t.Fatal(err)
	}
	forceResponse.Body.Close()
	if forceResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("confirmed force purge status = %d", forceResponse.StatusCode)
	}
}
