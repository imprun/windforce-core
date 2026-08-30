package opaquehttp

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"regexp"
	"strings"
)

const (
	MaxWireBodyBytes    int64 = 16 << 20
	maxJSONNestingDepth       = 16
)

var publicationRefPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func decodeInvocation(reader io.Reader, maxEnvelopeBytes int64, maxBodyBytes int64) (OpaqueHTTPInvocationV1, []byte, error) {
	raw, err := readBoundedJSON(reader, maxEnvelopeBytes)
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, err
	}
	if err := rejectDuplicateJSONMembers(raw); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, err
	}
	root, err := exactObject(raw, "kind", "trustedIngress", "http", "body", "receivedAt", "deadlineAt")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, err
	}
	if err := exactFieldTypes(root, []string{"kind", "receivedAt", "deadlineAt"}, nil); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, err
	}
	trusted, err := exactObject(root["trustedIngress"], "issuer", "audience", "publicationRef", "routeGeneration", "credentialRef")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("trustedIngress: %w", err)
	}
	if err := exactFieldTypes(trusted, []string{"issuer", "audience", "publicationRef"}, []string{"routeGeneration"}); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("trustedIngress: %w", err)
	}
	credentialRef, err := exactObject(trusted["credentialRef"], "id", "revision")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("trustedIngress.credentialRef: %w", err)
	}
	if err := exactFieldTypes(credentialRef, []string{"id", "revision"}, nil); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("trustedIngress.credentialRef: %w", err)
	}
	httpObject, err := exactObject(root["http"], "method", "exactEscapedPath", "contentType")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("http: %w", err)
	}
	if err := exactFieldTypes(httpObject, []string{"method", "exactEscapedPath", "contentType"}, nil); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("http: %w", err)
	}
	bodyObject, err := exactObject(root["body"], "encoding", "data", "byteLength", "digest")
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("body: %w", err)
	}
	if err := exactFieldTypes(bodyObject, []string{"encoding", "data", "digest"}, []string{"byteLength"}); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("body: %w", err)
	}
	var invocation OpaqueHTTPInvocationV1
	if err := json.Unmarshal(raw, &invocation); err != nil {
		return OpaqueHTTPInvocationV1{}, nil, fmt.Errorf("decode invocation: %w", err)
	}
	decoded, err := validateInvocation(invocation, maxBodyBytes)
	if err != nil {
		return OpaqueHTTPInvocationV1{}, nil, err
	}
	return invocation, decoded, nil
}

func validateInvocation(invocation OpaqueHTTPInvocationV1, maxBodyBytes int64) ([]byte, error) {
	if invocation.Kind != OpaqueHTTPInvocationKindV1 {
		return nil, errors.New("unsupported opaque HTTP invocation kind")
	}
	trusted := invocation.TrustedIngress
	if err := validateTrimmedString("trusted ingress issuer", trusted.Issuer, 160); err != nil {
		return nil, err
	}
	if err := validateTrimmedString("trusted ingress audience", trusted.Audience, 160); err != nil {
		return nil, err
	}
	if len(trusted.PublicationRef) == 0 || len(trusted.PublicationRef) > 100 || !publicationRefPattern.MatchString(trusted.PublicationRef) {
		return nil, errors.New("invalid publication reference")
	}
	if trusted.RouteGeneration < 1 {
		return nil, errors.New("route generation must be positive")
	}
	if err := validateImmutableRef(trusted.CredentialRef); err != nil {
		return nil, fmt.Errorf("credential reference: %w", err)
	}
	if err := validateHTTPMedia(invocation.HTTP); err != nil {
		return nil, err
	}
	return decodeBodyBytes(invocation.Body, maxBodyBytes)
}

func decodeApplicationWireResponse(raw []byte, maxBodyBytes int64) (ApplicationWireResponseV1, []byte, error) {
	if maxBodyBytes <= 0 || maxBodyBytes > MaxWireBodyBytes {
		return ApplicationWireResponseV1{}, nil, errors.New("opaque HTTP byte limit is invalid")
	}
	maxEnvelopeBytes := int64(base64.StdEncoding.EncodedLen(int(maxBodyBytes))) + envelopeOverheadBytes
	if int64(len(raw)) > maxEnvelopeBytes {
		return ApplicationWireResponseV1{}, nil, errors.New("application wire response exceeds the configured limit")
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return ApplicationWireResponseV1{}, nil, errors.New("application result is not valid JSON")
	}
	if err := rejectDuplicateJSONMembers(raw); err != nil {
		return ApplicationWireResponseV1{}, nil, err
	}
	root, err := exactObject(raw, "kind", "status", "headers", "body")
	if err != nil {
		return ApplicationWireResponseV1{}, nil, err
	}
	if err := exactFieldTypes(root, []string{"kind"}, []string{"status"}); err != nil {
		return ApplicationWireResponseV1{}, nil, err
	}
	bodyObject, err := exactObject(root["body"], "encoding", "data", "byteLength", "digest")
	if err != nil {
		return ApplicationWireResponseV1{}, nil, fmt.Errorf("body: %w", err)
	}
	if err := exactFieldTypes(bodyObject, []string{"encoding", "data", "digest"}, []string{"byteLength"}); err != nil {
		return ApplicationWireResponseV1{}, nil, fmt.Errorf("body: %w", err)
	}
	var headerValues []json.RawMessage
	if headerJSON := bytes.TrimSpace(root["headers"]); len(headerJSON) == 0 || headerJSON[0] != '[' {
		return ApplicationWireResponseV1{}, nil, errors.New("headers must be an array")
	}
	if err := json.Unmarshal(root["headers"], &headerValues); err != nil {
		return ApplicationWireResponseV1{}, nil, errors.New("headers must be an array")
	}
	for index, header := range headerValues {
		headerObject, err := exactObject(header, "name", "value")
		if err != nil {
			return ApplicationWireResponseV1{}, nil, fmt.Errorf("headers[%d]: %w", index, err)
		}
		if err := exactFieldTypes(headerObject, []string{"name", "value"}, nil); err != nil {
			return ApplicationWireResponseV1{}, nil, fmt.Errorf("headers[%d]: %w", index, err)
		}
	}
	var response ApplicationWireResponseV1
	if err := json.Unmarshal(raw, &response); err != nil {
		return ApplicationWireResponseV1{}, nil, fmt.Errorf("decode application wire response: %w", err)
	}
	if response.Kind != ApplicationWireResponseKindV1 {
		return ApplicationWireResponseV1{}, nil, errors.New("unsupported application wire response kind")
	}
	if response.Status < 100 || response.Status > 599 {
		return ApplicationWireResponseV1{}, nil, errors.New("application wire response status is out of range")
	}
	if len(response.Headers) > 1 {
		return ApplicationWireResponseV1{}, nil, errors.New("application wire response has too many headers")
	}
	for _, header := range response.Headers {
		if header.Name != "content-type" {
			return ApplicationWireResponseV1{}, nil, errors.New("application wire response header is not allowlisted")
		}
		if len(header.Value) > 2048 || hasControlCharacter(header.Value) {
			return ApplicationWireResponseV1{}, nil, errors.New("application wire response content-type is invalid")
		}
		if _, _, err := mime.ParseMediaType(header.Value); err != nil {
			return ApplicationWireResponseV1{}, nil, errors.New("application wire response content-type is invalid")
		}
	}
	decoded, err := decodeBodyBytes(response.Body, maxBodyBytes)
	if err != nil {
		return ApplicationWireResponseV1{}, nil, err
	}
	return response, decoded, nil
}

func encodeAppInput(invocation OpaqueHTTPInvocationV1) ([]byte, error) {
	return json.Marshal(OpaqueHTTPAppInputV1{
		Kind: OpaqueHTTPAppInputKindV1,
		HTTP: invocation.HTTP,
		Body: invocation.Body,
	})
}

func decodeBodyBytes(body BodyBytesV1, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > MaxWireBodyBytes {
		return nil, errors.New("opaque HTTP byte limit is invalid")
	}
	if body.Encoding != RFC4648Base64Encoding {
		return nil, errors.New("body encoding must be RFC4648-BASE64")
	}
	if body.ByteLength < 0 || body.ByteLength > maxBytes {
		return nil, errors.New("body byteLength exceeds the configured limit")
	}
	if int64(len(body.Data)) > int64(base64.StdEncoding.EncodedLen(int(maxBytes))) {
		return nil, errors.New("body Base64 encoding exceeds the configured limit")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(body.Data)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != body.Data {
		return nil, errors.New("body data must be canonical strict RFC 4648 Base64")
	}
	if int64(len(decoded)) != body.ByteLength {
		return nil, errors.New("body byteLength does not match decoded bytes")
	}
	digest := sha256.Sum256(decoded)
	expected := "sha256:" + hex.EncodeToString(digest[:])
	if !validSHA256(body.Digest) || subtle.ConstantTimeCompare([]byte(expected), []byte(body.Digest)) != 1 {
		return nil, errors.New("body digest does not match decoded bytes")
	}
	return decoded, nil
}

func validateHTTPMedia(media HTTPMediaV1) error {
	if len(media.Method) == 0 || len(media.Method) > 32 {
		return errors.New("HTTP method is invalid")
	}
	for _, character := range media.Method {
		if character < 'A' || character > 'Z' {
			return errors.New("HTTP method is invalid")
		}
	}
	if err := validateCanonicalEscapedPath(media.ExactEscapedPath); err != nil {
		return err
	}
	if len(media.ContentType) == 0 || len(media.ContentType) > 160 || hasControlCharacter(media.ContentType) {
		return errors.New("HTTP content type is invalid")
	}
	mediaType, _, err := mime.ParseMediaType(media.ContentType)
	if err != nil || !strings.Contains(mediaType, "/") || strings.Contains(mediaType, "*") {
		return errors.New("HTTP content type is invalid")
	}
	return nil
}

func validateCanonicalEscapedPath(path string) error {
	if len(path) == 0 || len(path) > 1024 || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("exact escaped path must start with exactly one slash")
	}
	if strings.Contains(path, "//") || strings.ContainsAny(path, `\?#`) {
		return errors.New("exact escaped path contains a forbidden delimiter")
	}
	if path != "/" && strings.HasSuffix(path, "/") {
		return errors.New("exact escaped path has a trailing slash")
	}
	for index := 0; index < len(path); index++ {
		character := path[index]
		if character > 0x7f {
			return errors.New("exact escaped path must contain ASCII bytes only")
		}
		if character != '%' {
			continue
		}
		if index+2 >= len(path) || !isUpperHex(path[index+1]) || !isUpperHex(path[index+2]) {
			return errors.New("exact escaped path contains malformed percent encoding")
		}
		decoded, _ := hex.DecodeString(path[index+1 : index+3])
		switch decoded[0] {
		case '.', '/', '\\', '%':
			return errors.New("exact escaped path contains a forbidden encoded delimiter")
		}
		index += 2
	}
	for _, segment := range strings.Split(path, "/")[1:] {
		if segment == "." || segment == ".." {
			return errors.New("exact escaped path contains a dot segment")
		}
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return errors.New("exact escaped path contains malformed percent encoding")
	}
	if strings.Contains(decoded, "*") {
		return errors.New("exact escaped path contains a wildcard marker")
	}
	if canonicalEscapedPath([]byte(decoded)) != path {
		return errors.New("exact escaped path is not canonical")
	}
	return nil
}

func canonicalEscapedPath(decoded []byte) string {
	var result strings.Builder
	for _, character := range decoded {
		if isPathSafe(character) {
			result.WriteByte(character)
			continue
		}
		result.WriteByte('%')
		const upperHex = "0123456789ABCDEF"
		result.WriteByte(upperHex[character>>4])
		result.WriteByte(upperHex[character&0x0f])
	}
	return result.String()
}

func isPathSafe(character byte) bool {
	if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' {
		return true
	}
	return strings.ContainsRune(`/:@-._~!$&'()+,;=`, rune(character))
}

func isUpperHex(character byte) bool {
	return character >= '0' && character <= '9' || character >= 'A' && character <= 'F'
}

func validateImmutableRef(ref ImmutableRefV1) error {
	if err := validateTrimmedString("immutable reference id", ref.ID, 200); err != nil {
		return err
	}
	if !validSHA256(ref.Revision) {
		return errors.New("immutable reference revision must be a lowercase SHA-256 digest")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	encoded := value[len("sha256:"):]
	decoded, err := hex.DecodeString(encoded)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(encoded) == encoded
}

func validateTrimmedString(name string, value string, maxLength int) error {
	if value == "" || strings.TrimSpace(value) != value || len(value) > maxLength || hasControlCharacter(value) {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

func hasControlCharacter(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func readBoundedJSON(reader io.Reader, maxBytes int64) ([]byte, error) {
	if reader == nil || maxBytes <= 0 {
		return nil, errors.New("request body is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, errors.New("could not read request body")
	}
	if int64(len(raw)) > maxBytes {
		return nil, errors.New("request envelope exceeds the configured limit")
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errors.New("request envelope must be one valid JSON value")
	}
	return raw, nil
}

func exactObject(raw []byte, fields ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, errors.New("value must be an object")
	}
	allowed := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		allowed[field] = struct{}{}
		if _, ok := object[field]; !ok {
			return nil, fmt.Errorf("missing required field %q", field)
		}
	}
	for field := range object {
		if _, ok := allowed[field]; !ok {
			return nil, fmt.Errorf("unknown field %q", field)
		}
	}
	return object, nil
}

func exactFieldTypes(object map[string]json.RawMessage, stringFields []string, integerFields []string) error {
	for _, field := range stringFields {
		raw := bytes.TrimSpace(object[field])
		if len(raw) == 0 || raw[0] != '"' {
			return fmt.Errorf("field %q must be a string", field)
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("field %q must be a string", field)
		}
	}
	for _, field := range integerFields {
		raw := bytes.TrimSpace(object[field])
		if len(raw) == 0 || raw[0] == 'n' {
			return fmt.Errorf("field %q must be an integer", field)
		}
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("field %q must be an integer", field)
		}
	}
	return nil
}

func rejectDuplicateJSONMembers(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request envelope has a trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONNestingDepth {
		return errors.New("JSON value exceeds the maximum nesting depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object member name must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
