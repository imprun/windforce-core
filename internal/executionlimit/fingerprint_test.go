package executionlimit

import (
	"strings"
	"testing"
)

func TestFingerprintIsStableAndExcludesCapacity(t *testing.T) {
	shape := Shape{
		WorkspaceID:   "workspace-a",
		AppKey:        "orders",
		ActionKey:     "collect",
		Scope:         ScopeAction,
		PolicyID:      "account",
		Kind:          KindConcurrency,
		InputPointers: []string{"/account/id", "/region"},
	}
	first, err := Fingerprint(shape)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(shape)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !IsFingerprint(first) {
		t.Fatalf("fingerprints = %q, %q", first, second)
	}
	if !strings.HasPrefix(first, FingerprintPrefix) {
		t.Fatalf("fingerprint %q does not expose its version and algorithm", first)
	}
}

func TestFingerprintSeparatesAmbiguousTuplesAndPointerOrder(t *testing.T) {
	base := Shape{WorkspaceID: "default", AppKey: "ab", Scope: ScopeApp, PolicyID: "c", Kind: KindConcurrency, InputPointers: []string{"/a", "/b"}}
	first, err := Fingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	ambiguous := base
	ambiguous.AppKey = "a"
	ambiguous.PolicyID = "bc"
	second, err := Fingerprint(ambiguous)
	if err != nil {
		t.Fatal(err)
	}
	reordered := base
	reordered.InputPointers = []string{"/b", "/a"}
	third, err := Fingerprint(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == third {
		t.Fatalf("distinct shapes collided: %q %q %q", first, second, third)
	}
}

func TestFingerprintIncludesRateWindowAndNormalizesAppScopeAction(t *testing.T) {
	base := Shape{WorkspaceID: "default", AppKey: "orders", ActionKey: "collect", Scope: ScopeApp, PolicyID: "account", Kind: KindRate, WindowSeconds: 60, InputPointers: []string{"/account"}}
	first, err := Fingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	base.WindowSeconds = 120
	second, err := Fingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("rate windows must participate in shape identity")
	}
	base.WindowSeconds = 60
	base.ActionKey = "other"
	third, err := Fingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	if first != third {
		t.Fatal("app-scoped shapes must canonicalize the action field to none")
	}
}

func TestImplicitAppConcurrencyFingerprint(t *testing.T) {
	value, err := AppConcurrencyFingerprint("", "orders")
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := Fingerprint(Shape{WorkspaceID: "default", AppKey: "orders", Scope: ScopeApp, PolicyID: ImplicitAppConcurrencyPolicyID, Kind: KindConcurrency})
	if err != nil {
		t.Fatal(err)
	}
	if value != explicit {
		t.Fatalf("implicit = %q, explicit = %q", value, explicit)
	}
}

func TestFingerprintRejectsInvalidShapes(t *testing.T) {
	cases := []Shape{
		{WorkspaceID: "default", Scope: ScopeApp, PolicyID: "x", Kind: KindConcurrency},
		{WorkspaceID: "default", AppKey: "app", Scope: ScopeAction, PolicyID: "x", Kind: KindConcurrency},
		{WorkspaceID: "default", AppKey: "app", Scope: ScopeApp, PolicyID: "x", Kind: KindRate},
		{WorkspaceID: "default", AppKey: "app", Scope: ScopeApp, PolicyID: "x", Kind: KindConcurrency, InputPointers: []string{"bad"}},
	}
	for _, shape := range cases {
		if _, err := Fingerprint(shape); err == nil {
			t.Fatalf("Fingerprint(%+v) succeeded", shape)
		}
	}
}
