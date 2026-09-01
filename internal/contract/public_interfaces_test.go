package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizePublicInterfacesUsesCanonicalJSONIdentity(t *testing.T) {
	normalized, err := NormalizePublicInterfaces([]json.RawMessage{
		json.RawMessage(` { "z": [3, 2, 1], "a": { "enabled": true, "count": 1 } } `),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(normalized[0]), `{"a":{"count":1,"enabled":true},"z":[3,2,1]}`; got != want {
		t.Fatalf("canonical declaration = %s, want %s", got, want)
	}

	_, err = NormalizePublicInterfaces([]json.RawMessage{
		json.RawMessage(`{"a":1,"b":2}`),
		json.RawMessage(` { "b": 2, "a": 1 } `),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestNormalizePublicInterfacesRejectsUnsafeShapes(t *testing.T) {
	tooMany := make([]json.RawMessage, MaxPublicInterfacesPerAction+1)
	for index := range tooMany {
		tooMany[index] = json.RawMessage(`{"id":` + string(rune('0'+index%10)) + `}`)
	}
	tooLargeInAggregate := make([]json.RawMessage, 5)
	for index := range tooLargeInAggregate {
		tooLargeInAggregate[index] = json.RawMessage(`{"id":` + string(rune('0'+index)) + `,"value":"` + strings.Repeat("a", MaxPublicInterfaceDeclarationBytes-128) + `"}`)
	}
	tooDeep := json.RawMessage(`{"value":` + strings.Repeat("[", MaxPublicInterfaceJSONDepth) + `0` + strings.Repeat("]", MaxPublicInterfaceJSONDepth) + `}`)
	tests := map[string]struct {
		declarations []json.RawMessage
		want         string
	}{
		"non-object": {
			declarations: []json.RawMessage{json.RawMessage(`[1,2,3]`)},
			want:         "must be a JSON object",
		},
		"duplicate object key": {
			declarations: []json.RawMessage{json.RawMessage(`{"id":"a","id":"b"}`)},
			want:         "duplicate object key",
		},
		"oversized": {
			declarations: []json.RawMessage{json.RawMessage(`{"value":"` + strings.Repeat("a", MaxPublicInterfaceDeclarationBytes) + `"}`)},
			want:         "maximum",
		},
		"too many declarations": {
			declarations: tooMany,
			want:         "maximum",
		},
		"aggregate size": {
			declarations: tooLargeInAggregate,
			want:         "maximum",
		},
		"nesting depth": {
			declarations: []json.RawMessage{tooDeep},
			want:         "maximum depth",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NormalizePublicInterfaces(test.declarations)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCloneDeploymentDetachesPublicInterfaces(t *testing.T) {
	declaration := json.RawMessage(`{"contract":"example/v1"}`)
	want := string(declaration)
	deployment := Deployment{
		APIVersion: AppManifestV2,
		Actions: map[string]Action{
			"run": {PublicInterfaces: []json.RawMessage{declaration}},
		},
	}
	cloned := CloneDeployment(deployment)
	cloned.Actions["run"].PublicInterfaces[0][2] = 'X'
	if got := string(deployment.Actions["run"].PublicInterfaces[0]); got != want {
		t.Fatalf("source declaration mutated: %s", got)
	}
}

func TestNormalizeDeploymentPublicInterfacesRequiresV2ForExplicitEmptyArray(t *testing.T) {
	_, err := NormalizeDeploymentPublicInterfaces(Deployment{
		Actions: map[string]Action{
			"run": {PublicInterfaces: []json.RawMessage{}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "requires apiVersion") {
		t.Fatalf("error = %v", err)
	}
}
