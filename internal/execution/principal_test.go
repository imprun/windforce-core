package execution

import "testing"

func TestOpaqueHTTPProjectionScopesAreValid(t *testing.T) {
	scopes := []string{
		string(ScopeOpaqueHTTPProjectionsRead),
		string(ScopeOpaqueHTTPProjectionsWrite),
	}
	if !ValidScopeSet(scopes) {
		t.Fatalf("ValidScopeSet(%#v) = false", scopes)
	}

	for _, invalid := range []string{
		"opaque_http_projections:read",
		"opaque-http-projection:write",
		"opaque-http-projections:admin",
	} {
		if ValidScopeSet([]string{invalid}) {
			t.Fatalf("ValidScopeSet accepted %q", invalid)
		}
	}
}
