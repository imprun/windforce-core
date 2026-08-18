package server

import (
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestMaterializeActorRuntimeAccessMapsOnlyCurrentSubject(t *testing.T) {
	access := contract.RuntimeAccess{
		VariableTargets: []contract.RuntimeConfigTarget{{Scope: contract.RuntimeConfigScopeActor, Path: "connections/account"}},
		WriteVariables: []contract.RuntimeVariableWriteTarget{{
			RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeActor, Path: "connections/session"},
			Storage:             contract.RuntimeVariableStorageSecret,
		}},
	}
	alpha, err := materializeActorRuntimeAccess(access, "account:alpha")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := materializeActorRuntimeAccess(access, "account:beta")
	if err != nil {
		t.Fatal(err)
	}
	if alpha.VariableTargets[0].Scope != contract.RuntimeConfigScopeApp || alpha.WriteVariables[0].Scope != contract.RuntimeConfigScopeApp {
		t.Fatalf("materialized access = %#v", alpha)
	}
	if alpha.VariableTargets[0].Path == beta.VariableTargets[0].Path || alpha.WriteVariables[0].Path == beta.WriteVariables[0].Path {
		t.Fatal("different subjects resolved to the same physical path")
	}
}
