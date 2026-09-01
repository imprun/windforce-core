package server

import (
	"strings"
	"testing"
)

func TestReadStrictJSONBodyRejectsUnknownDuplicateAndOversizedInput(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name     string
		body     string
		maxBytes int64
	}{
		{name: "unknown", body: `{"name":"ok","secret":"must-not-be-accepted"}`, maxBytes: 1024},
		{name: "duplicate", body: `{"name":"first","name":"second"}`, maxBytes: 1024},
		{name: "oversized", body: `{"name":"too-large"}`, maxBytes: 4},
		{name: "trailing", body: `{"name":"ok"} {"name":"other"}`, maxBytes: 1024},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var decoded request
			if err := readStrictJSONBody(strings.NewReader(test.body), test.maxBytes, &decoded); err == nil {
				t.Fatal("expected strict JSON rejection")
			}
		})
	}
}

func TestReadStrictJSONBodyAcceptsExactObject(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	var decoded request
	if err := readStrictJSONBody(strings.NewReader(`{"name":"projection"}`), 1024, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Name != "projection" {
		t.Fatalf("name=%q", decoded.Name)
	}
}
