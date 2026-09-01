package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	// OpaqueIngressProjectionDigestCanonicalization is the portable JSON
	// canonicalization used by projection publishers and Core.
	OpaqueIngressProjectionDigestCanonicalization = "projection-ascii-subset/v1"

	opaqueIngressCredentialDigestSchema  = "windforce-core.opaque-ingress-credential-snapshot/v1"
	opaqueIngressPublicationDigestSchema = "windforce-core.opaque-ingress-publication-revision/v1"
)

type opaqueIngressCredentialDigestReference struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type opaqueIngressResponsePolicyDigestMaterial struct {
	AllowMissingContentType bool     `json:"allowMissingContentType"`
	ContentTypes            []string `json:"contentTypes"`
	MaxBodyBytes            int64    `json:"maxBodyBytes"`
}

type opaqueIngressHTTPDigestMaterial struct {
	ContentType         string                                    `json:"contentType"`
	ExactEscapedPath    string                                    `json:"exactEscapedPath"`
	MaxRequestBodyBytes int64                                     `json:"maxRequestBodyBytes"`
	Method              string                                    `json:"method"`
	ResponsePolicy      opaqueIngressResponsePolicyDigestMaterial `json:"responsePolicy"`
}

type opaqueIngressCredentialDigestMaterial struct {
	Audience            string                                 `json:"audience"`
	MaxStalenessSeconds int64                                  `json:"maxStalenessSeconds"`
	NotAfter            string                                 `json:"notAfter"`
	OperationRef        string                                 `json:"operationRef"`
	ProjectedAt         string                                 `json:"projectedAt"`
	Reference           opaqueIngressCredentialDigestReference `json:"reference"`
	References          []contract.NamedImmutableReferencePin  `json:"references"`
	Schema              string                                 `json:"schema"`
	WorkspaceID         string                                 `json:"workspaceId"`
	Issuer              string                                 `json:"issuer"`
}

type opaqueIngressPublicationDigestMaterial struct {
	Action              string                                `json:"action"`
	App                 string                                `json:"app"`
	Audience            string                                `json:"audience"`
	CredentialRefs      []OpaqueIngressCredentialSnapshotRef  `json:"credentialRefs"`
	HTTP                opaqueIngressHTTPDigestMaterial       `json:"http"`
	Issuer              string                                `json:"issuer"`
	MaxStalenessSeconds int64                                 `json:"maxStalenessSeconds"`
	NotAfter            string                                `json:"notAfter"`
	OperationRef        string                                `json:"operationRef"`
	ProjectedAt         string                                `json:"projectedAt"`
	PublicationRef      string                                `json:"publicationRef"`
	References          []contract.NamedImmutableReferencePin `json:"references"`
	Release             OpaqueIngressReleasePin               `json:"release"`
	RetainUntil         string                                `json:"retainUntil"`
	Revision            string                                `json:"revision"`
	Schema              string                                `json:"schema"`
	WorkspaceID         string                                `json:"workspaceId"`
}

// OpaqueIngressCredentialSnapshotDigest returns the portable content digest
// over the immutable snapshot material. The self digest and all mutation
// metadata are excluded.
func OpaqueIngressCredentialSnapshotDigest(snapshot OpaqueIngressCredentialSnapshot) string {
	return opaqueIngressCanonicalDigest(opaqueIngressCredentialDigestMaterialFor(snapshot))
}

func opaqueIngressCredentialDigestMaterialFor(snapshot OpaqueIngressCredentialSnapshot) opaqueIngressCredentialDigestMaterial {
	return opaqueIngressCredentialDigestMaterial{
		Audience:            snapshot.Audience,
		Issuer:              snapshot.Issuer,
		MaxStalenessSeconds: snapshot.MaxStalenessSeconds,
		NotAfter:            snapshot.NotAfter.UTC().Format(canonicalOpaqueIngressTimeLayout),
		OperationRef:        snapshot.OperationRef,
		ProjectedAt:         snapshot.ProjectedAt.UTC().Format(canonicalOpaqueIngressTimeLayout),
		Reference:           opaqueIngressCredentialDigestReference{ID: snapshot.Reference.ID, Revision: snapshot.Reference.Revision},
		References:          append([]contract.NamedImmutableReferencePin{}, snapshot.References...),
		Schema:              opaqueIngressCredentialDigestSchema,
		WorkspaceID:         snapshot.WorkspaceID,
	}
}

// OpaqueIngressPublicationRevisionDigest returns the portable content digest
// over the immutable publication material. The self digest and all mutation
// metadata are excluded.
func OpaqueIngressPublicationRevisionDigest(revision OpaqueIngressPublicationRevision) string {
	return opaqueIngressCanonicalDigest(opaqueIngressPublicationDigestMaterialFor(revision))
}

func opaqueIngressPublicationDigestMaterialFor(revision OpaqueIngressPublicationRevision) opaqueIngressPublicationDigestMaterial {
	return opaqueIngressPublicationDigestMaterial{
		Action:         revision.Action,
		App:            revision.App,
		Audience:       revision.Audience,
		CredentialRefs: append([]OpaqueIngressCredentialSnapshotRef{}, revision.CredentialRefs...),
		HTTP: opaqueIngressHTTPDigestMaterial{
			ContentType:         revision.HTTP.ContentType,
			ExactEscapedPath:    revision.HTTP.ExactEscapedPath,
			MaxRequestBodyBytes: revision.HTTP.MaxRequestBodyBytes,
			Method:              revision.HTTP.Method,
			ResponsePolicy: opaqueIngressResponsePolicyDigestMaterial{
				AllowMissingContentType: revision.HTTP.ResponsePolicy.AllowMissingContentType,
				ContentTypes:            append([]string{}, revision.HTTP.ResponsePolicy.ContentTypes...),
				MaxBodyBytes:            revision.HTTP.ResponsePolicy.MaxBodyBytes,
			},
		},
		Issuer:              revision.Issuer,
		MaxStalenessSeconds: revision.MaxStalenessSeconds,
		NotAfter:            revision.NotAfter.UTC().Format(canonicalOpaqueIngressTimeLayout),
		OperationRef:        revision.OperationRef,
		ProjectedAt:         revision.ProjectedAt.UTC().Format(canonicalOpaqueIngressTimeLayout),
		PublicationRef:      revision.PublicationRef,
		References:          append([]contract.NamedImmutableReferencePin{}, revision.References...),
		Release:             revision.Release,
		RetainUntil:         revision.RetainUntil.UTC().Format(canonicalOpaqueIngressTimeLayout),
		Revision:            revision.Revision,
		Schema:              opaqueIngressPublicationDigestSchema,
		WorkspaceID:         revision.WorkspaceID,
	}
}

const canonicalOpaqueIngressTimeLayout = "2006-01-02T15:04:05.999999999Z"

func opaqueIngressCanonicalDigest(material any) string {
	canonical, err := canonicalOpaqueIngressJSON(material)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func canonicalOpaqueIngressJSON(material any) ([]byte, error) {
	raw, err := json.Marshal(material)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return nil, err
	}
	return []byte(strings.TrimSuffix(output.String(), "\n")), nil
}
