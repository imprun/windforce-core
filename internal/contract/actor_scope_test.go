package contract

import (
	"strings"
	"testing"
)

func TestActorRuntimeConfigPathIsStableAndSubjectIsolated(t *testing.T) {
	first, err := ActorRuntimeConfigPath("account:alpha", "connections/tistory/session")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := ActorRuntimeConfigPath("account:alpha", "connections/tistory/session")
	if err != nil {
		t.Fatal(err)
	}
	other, err := ActorRuntimeConfigPath("account:beta", "connections/tistory/session")
	if err != nil {
		t.Fatal(err)
	}
	if first != repeated || first == other {
		t.Fatalf("actor paths are not stable and isolated: %q %q %q", first, repeated, other)
	}
	if strings.Contains(first, "account") || strings.Contains(first, "alpha") {
		t.Fatalf("actor path leaks subject: %q", first)
	}
	if _, err := ActorRuntimeConfigPath("", "connections/tistory/session"); err == nil {
		t.Fatal("missing actor subject was accepted")
	}
}

func TestNormalizeRuntimeAccessAcceptsActorTargets(t *testing.T) {
	access, err := NormalizeRuntimeAccess(RuntimeAccess{
		VariableTargets: []RuntimeConfigTarget{{Scope: RuntimeConfigScopeActor, Path: "connections/session"}},
		WriteVariables: []RuntimeVariableWriteTarget{{
			RuntimeConfigTarget: RuntimeConfigTarget{Scope: RuntimeConfigScopeActor, Path: "connections/session"},
			Storage:             RuntimeVariableStorageSecret,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(access.VariableTargets) != 1 || access.VariableTargets[0].Scope != RuntimeConfigScopeActor ||
		len(access.WriteVariables) != 1 || access.WriteVariables[0].Scope != RuntimeConfigScopeActor {
		t.Fatalf("normalized actor access = %#v", access)
	}
}
