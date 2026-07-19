package contract

import (
	"strings"
	"testing"
)

func TestValidWorkspaceID(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"default": true,
		"team-a":  true,
		"a1":      true,
		"a":       false,
		"Team-a":  false,
		"team_a":  false,
		"-team":   false,
		"team-":   false,
		"1team":   false,
	}
	for value, want := range tests {
		if got := ValidWorkspaceID(value); got != want {
			t.Errorf("ValidWorkspaceID(%q) = %v, want %v", value, got, want)
		}
	}

	if ValidWorkspaceID("a" + strings.Repeat("b", 48)) {
		t.Fatal("workspace id longer than 48 characters is valid")
	}
}
