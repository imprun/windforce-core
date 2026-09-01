package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

type opaqueIngressDigestGoldenFile struct {
	Canonicalization string `json:"canonicalization"`
	Vectors          []struct {
		Name     string          `json:"name"`
		Material json.RawMessage `json:"material"`
		Digest   string          `json:"digest"`
	} `json:"vectors"`
}

func TestOpaqueIngressProjectionDigestV1GoldenVectors(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/opaque_ingress_projection_digest_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden opaqueIngressDigestGoldenFile
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Canonicalization != OpaqueIngressProjectionDigestCanonicalization {
		t.Fatalf("canonicalization = %q", golden.Canonicalization)
	}
	if len(golden.Vectors) != 4 {
		t.Fatalf("vectors = %d, want 4", len(golden.Vectors))
	}

	credential, publication := opaqueIngressDigestGoldenValues(t)
	credentialWithoutReferences := credential
	credentialWithoutReferences.References = nil
	publicationWithoutReferences := publication
	publicationWithoutReferences.References = nil
	materials := map[string]any{
		"credential-snapshot-v1":                   opaqueIngressCredentialDigestMaterialFor(credential),
		"publication-revision-v1":                  opaqueIngressPublicationDigestMaterialFor(publication),
		"credential-snapshot-empty-references-v1":  opaqueIngressCredentialDigestMaterialFor(credentialWithoutReferences),
		"publication-revision-empty-references-v1": opaqueIngressPublicationDigestMaterialFor(publicationWithoutReferences),
	}
	digests := map[string]string{
		"credential-snapshot-v1":                   OpaqueIngressCredentialSnapshotDigest(credential),
		"publication-revision-v1":                  OpaqueIngressPublicationRevisionDigest(publication),
		"credential-snapshot-empty-references-v1":  OpaqueIngressCredentialSnapshotDigest(credentialWithoutReferences),
		"publication-revision-empty-references-v1": OpaqueIngressPublicationRevisionDigest(publicationWithoutReferences),
	}
	for _, vector := range golden.Vectors {
		material, ok := materials[vector.Name]
		if !ok {
			t.Fatalf("unknown vector %q", vector.Name)
		}
		gotCanonical, err := canonicalOpaqueIngressJSON(material)
		if err != nil {
			t.Fatal(err)
		}
		wantCanonical, err := canonicalOpaqueIngressJSON(vector.Material)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotCanonical) != string(wantCanonical) {
			t.Fatalf("%s canonical material mismatch\ngot  %s\nwant %s", vector.Name, gotCanonical, wantCanonical)
		}
		if got := digests[vector.Name]; got != vector.Digest {
			t.Fatalf("%s digest = %q, want %q", vector.Name, got, vector.Digest)
		}
	}
}

func opaqueIngressDigestGoldenValues(t *testing.T) (OpaqueIngressCredentialSnapshot, OpaqueIngressPublicationRevision) {
	t.Helper()
	projectedAt := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	credential := OpaqueIngressCredentialSnapshot{
		WorkspaceID: "default", Issuer: "gateway.example", Audience: "windforce-core",
		Reference: OpaqueIngressCredentialSnapshotRef{
			ID: "credential/customer-a", Revision: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		OperationRef: "identity/verify",
		References: []contract.NamedImmutableReferencePin{{
			Name: "customerProfile", Reference: contract.ImmutableReference{ID: "profile/customer-a", Version: "v1"},
		}},
		ProjectedAt: projectedAt, NotAfter: projectedAt.Add(time.Hour), MaxStalenessSeconds: 3600,
	}
	credential.Reference.Digest = OpaqueIngressCredentialSnapshotDigest(credential)
	publication := OpaqueIngressPublicationRevision{
		WorkspaceID: "default", Issuer: "gateway.example", Audience: "windforce-core",
		PublicationRef: "identity-check", Revision: "revision-1", App: "identity_verification", Action: "verify",
		Release: OpaqueIngressReleasePin{
			DeploymentID: "deployment-1", Commit: "0123456789abcdef",
			BundleDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		HTTP: OpaqueIngressHTTPContract{
			Method: "POST", ExactEscapedPath: "/v2/identity/check", ContentType: "application/json", MaxRequestBodyBytes: 4096,
			ResponsePolicy: contract.HTTPPolicy{ContentTypes: []string{"application/json"}, MaxBodyBytes: 4096},
		},
		OperationRef: "identity/verify", CredentialRefs: []OpaqueIngressCredentialSnapshotRef{credential.Reference},
		References: []contract.NamedImmutableReferencePin{{
			Name: "routeSchema", Reference: contract.ImmutableReference{ID: "schema/identity-check", Version: "v1"},
		}},
		ProjectedAt: projectedAt, NotAfter: projectedAt.Add(time.Hour), MaxStalenessSeconds: 3600,
		RetainUntil: projectedAt.Add(24 * time.Hour),
	}
	return credential, publication
}
