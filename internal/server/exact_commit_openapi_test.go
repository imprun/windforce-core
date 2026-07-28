package server

import "testing"

func TestControlPlaneOpenAPIAdvertisesExactCommitPreconditions(t *testing.T) {
	document := buildControlPlaneOpenAPI("http://core.example.test", "default")
	paths := document["paths"].(map[string]any)

	for path, wantSchema := range map[string]string{
		"/api/w/{workspace}/git_sources/{gitSourceId}/sync":   "SyncGitSourceRequest",
		"/api/w/{workspace}/git_sources/{gitSourceId}/deploy": "DeployGitSourceRequest",
	} {
		post := paths[path].(map[string]any)["post"].(map[string]any)
		requestBody := post["requestBody"].(map[string]any)
		content := requestBody["content"].(map[string]any)
		schema := content["application/json"].(map[string]any)["schema"].(map[string]any)
		if got := schema["$ref"]; got != "#/components/schemas/"+wantSchema {
			t.Fatalf("%s request schema = %v, want %s", path, got, wantSchema)
		}
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"SyncGitSourceRequest", "DeployGitSourceRequest"} {
		properties := schemas[name].(map[string]any)["properties"].(map[string]any)
		if _, ok := properties["expected_commit"]; !ok {
			t.Errorf("%s does not advertise expected_commit", name)
		}
		if _, ok := properties["commit"]; ok {
			t.Errorf("%s must not advertise the unsupported commit selector", name)
		}
	}

	deployResult := schemas["GitSourceDeployResult"].(map[string]any)
	deployProperties := deployResult["properties"].(map[string]any)
	if _, ok := deployProperties["release_id"]; !ok {
		t.Error("GitSourceDeployResult does not advertise release_id")
	}
	required := deployResult["required"].([]any)
	if !containsOpenAPIName(required, "release_id") {
		t.Error("GitSourceDeployResult does not require release_id")
	}
}

func containsOpenAPIName(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
