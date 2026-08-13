package state

import (
	"reflect"
	"testing"
)

func TestNormalizeTargetPolicy(t *testing.T) {
	policy, err := NormalizeTargetPolicy(TargetPolicy{
		Mode:           TargetPolicyModeRestricted,
		AllowedTargets: []string{"reports/export", "orders", "orders", " reports/export "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.Mode != TargetPolicyModeRestricted || !reflect.DeepEqual(policy.AllowedTargets, []string{"orders", "reports/export"}) {
		t.Fatalf("policy = %#v", policy)
	}
	if _, err := NormalizeTargetPolicy(TargetPolicy{Mode: TargetPolicyModeAll, AllowedTargets: []string{"orders"}}); err == nil {
		t.Fatal("all policy accepted allowed targets")
	}
	for _, target := range []string{"", "/run", "app/", "app/action/extra", "Bad App", "app/Bad Action"} {
		if _, err := NormalizeTargetPolicy(TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: []string{target}}); err == nil {
			t.Fatalf("invalid target %q was accepted", target)
		}
	}
}

func TestEffectiveTargetPolicyCompatibilityAndFailClosed(t *testing.T) {
	legacy := EffectiveTargetPolicy(TargetPolicy{})
	if legacy.Mode != TargetPolicyModeAll || !legacy.Allows("orders", "create") {
		t.Fatalf("legacy policy = %#v", legacy)
	}
	corrupt := EffectiveTargetPolicy(TargetPolicy{Mode: "unknown"})
	if corrupt.Mode != TargetPolicyModeRestricted || corrupt.Allows("orders", "create") {
		t.Fatalf("corrupt policy did not fail closed: %#v", corrupt)
	}
	restricted := TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: []string{"orders", "reports/export"}}
	if !restricted.Allows("orders", "create") || !restricted.Allows("reports", "export") ||
		!restricted.Allows("reports", "") || restricted.Allows("reports", "delete") || restricted.Allows("other", "run") {
		t.Fatalf("restricted policy has unexpected authorization: %#v", restricted)
	}
}
