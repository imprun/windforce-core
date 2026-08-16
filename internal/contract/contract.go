package contract

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	DefaultWorkspace   = "default"
	DefaultGitSourceID = "local"
	DefaultRouteTag    = "default"
	DefaultTimeoutS    = int32(300)

	ScriptLangTypeScript = "typescript"
	ScriptLangPython     = "python"
	ScriptLangGo         = "go"

	ActionAdapterJSONFile = "json-file"
	ActionAdapterCommand  = "command"

	CapabilityBrowser = "browser"

	MaxKeyedConcurrencyLimits   = 8
	MaxKeyedRateLimits          = 8
	MaxExecutionInputPointers   = 8
	MaxConcurrencyInputPointers = MaxExecutionInputPointers
	MinRateWindowSeconds        = 1
	MaxRateWindowSeconds        = 86400
	MaxConcurrencyPointerBytes  = 256
)

// NormalizeScriptLanguage applies the backwards-compatible TypeScript default
// while rejecting languages for which Core has no launcher contract. Unknown
// values must never fall through to Bun implicitly.
func NormalizeScriptLanguage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ScriptLangTypeScript, nil
	}
	switch value {
	case ScriptLangTypeScript, ScriptLangPython, ScriptLangGo:
		return value, nil
	default:
		return "", fmt.Errorf(
			"unsupported scriptLang %q; supported values are %s, %s, and %s",
			value,
			ScriptLangTypeScript,
			ScriptLangPython,
			ScriptLangGo,
		)
	}
}

// Labels are the open worker-matching vocabulary (ADR 0009). The sys/
// prefix is reserved for operator-granted placement labels and is
// rejected in author manifests.
const (
	MaxLabels           = 16
	ReservedLabelPrefix = "sys/"
)

// Engine-owned bearer credentials carry a "wf"-family prefix. This is a public
// contract for fronting platforms/proxies: such a credential can only be
// verified by the engine that minted or was configured with it (the secret
// never leaves the engine), so a proxy that cannot verify it classifies by prefix and
// forwards it unswapped for the engine to enforce. New token kinds MUST
// join CellBearerTokenPrefixes and keep the family prefix; platform layers
// must not mint tokens in the wf namespace.
const (
	JobTokenPrefix          = "wfjob_"
	WorkspaceTokenPrefix    = "wfw_"
	ClientTokenPrefix       = "wfk_"
	ServiceTokenPrefix      = "wfs_"
	RemoteWorkerTokenPrefix = "wfr_"
)

// CellBearerTokenPrefixes lists every engine-owned bearer prefix — the
// pass-through classification contract for fronting proxies.
func CellBearerTokenPrefixes() []string {
	return []string{
		JobTokenPrefix,
		WorkspaceTokenPrefix,
		ClientTokenPrefix,
		ServiceTokenPrefix,
		RemoteWorkerTokenPrefix,
	}
}

// IsCellBearerToken reports whether a presented bearer belongs to the engine
// and therefore can only be verified by it.
func IsCellBearerToken(token string) bool {
	for _, prefix := range CellBearerTokenPrefixes() {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

var labelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)

// App is the deployable source bundle described by windforce.json.
type App struct {
	App        string `json:"app"`
	Name       string `json:"name,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	ScriptLang string `json:"scriptLang,omitempty"`
	TimeoutS   int32  `json:"timeout,omitempty"`
	Tag        string `json:"tag,omitempty"`
	// MaxConcurrent caps concurrently running jobs for this app. Nil means unlimited.
	MaxConcurrent   *int32          `json:"maxConcurrent,omitempty"`
	ExecutionLimits ExecutionLimits `json:"executionLimits,omitempty,omitzero"`
	Capabilities    []string        `json:"capabilities,omitempty"`
	// RunsOn is the required worker label set (ADR 0009); capabilities
	// merge into it as an alias during manifest parsing.
	RunsOn  []string          `json:"runsOn,omitempty"`
	Actions map[string]Action `json:"actions"`
}

// Action is one executable unit inside an app.
type Action struct {
	Action                 string         `json:"action"`
	Tag                    *string        `json:"tag,omitempty"`
	TagOverride            *string        `json:"tagOverride,omitempty"`
	RequiredLabelsOverride *[]string      `json:"requiredLabelsOverride,omitempty"`
	Runtime                string         `json:"runtime,omitempty"`
	Entrypoint             string         `json:"entrypoint,omitempty"`
	Command                []string       `json:"command,omitempty"`
	Adapter                *ActionAdapter `json:"adapter,omitempty"`
	InputSchema            string         `json:"inputSchema,omitempty"`
	OutputSchema           string         `json:"outputSchema,omitempty"`
	// OperatorSettingsSchema documents release-owned input settings that are
	// not part of the public action request body.
	OperatorSettingsSchema string `json:"operatorSettingsSchema,omitempty"`
	// Materialized schema bodies are pinned during sync for control-plane reads.
	InputSchemaBody            json.RawMessage `json:"inputSchemaBody,omitempty"`
	OutputSchemaBody           json.RawMessage `json:"outputSchemaBody,omitempty"`
	OperatorSettingsSchemaBody json.RawMessage `json:"operatorSettingsSchemaBody,omitempty"`
	TimeoutS                   *int32          `json:"timeout,omitempty"`
	TimeoutMs                  int64           `json:"timeoutMs,omitempty"`
	Capabilities               *[]string       `json:"capabilities,omitempty"`
	RunsOn                     *[]string       `json:"runsOn,omitempty"`
	RuntimeAccess              RuntimeAccess   `json:"runtimeAccess,omitempty"`
	ExecutionLimits            ExecutionLimits `json:"executionLimits,omitempty,omitzero"`
	UpdatedAt                  *time.Time      `json:"updatedAt,omitempty"`
}

// ExecutionLimits contains release-owned, domain-neutral execution limits.
// Admission resolves each declaration to opaque pins before a Job is stored.
type ExecutionLimits struct {
	Concurrency []KeyedConcurrencyLimit `json:"concurrency,omitempty"`
	Rate        []KeyedRateLimit        `json:"rate,omitempty"`
}

// KeyedConcurrencyLimit caps leased Jobs that resolve to the same opaque key.
// InputPointers are RFC 6901 JSON Pointers evaluated against resolved input.
type KeyedConcurrencyLimit struct {
	ID            string   `json:"id"`
	MaxConcurrent int32    `json:"maxConcurrent"`
	InputPointers []string `json:"inputPointers"`
}

// KeyedRateLimit caps successful execution-attempt claims that resolve to the
// same opaque key inside one epoch-aligned fixed window.
type KeyedRateLimit struct {
	ID            string   `json:"id"`
	MaxAttempts   int32    `json:"maxAttempts"`
	WindowSeconds int32    `json:"windowSeconds"`
	InputPointers []string `json:"inputPointers"`
}

// NormalizeExecutionLimits validates and defensively copies release-owned
// execution-limit declarations. Pointer syntax is checked here; values are
// resolved only after Admission has produced the effective input.
func NormalizeExecutionLimits(limits ExecutionLimits) (ExecutionLimits, error) {
	if len(limits.Concurrency) > MaxKeyedConcurrencyLimits {
		return ExecutionLimits{}, fmt.Errorf("concurrency declares %d limits; maximum is %d", len(limits.Concurrency), MaxKeyedConcurrencyLimits)
	}
	if len(limits.Rate) > MaxKeyedRateLimits {
		return ExecutionLimits{}, fmt.Errorf("rate declares %d limits; maximum is %d", len(limits.Rate), MaxKeyedRateLimits)
	}
	normalized := ExecutionLimits{
		Concurrency: make([]KeyedConcurrencyLimit, 0, len(limits.Concurrency)),
		Rate:        make([]KeyedRateLimit, 0, len(limits.Rate)),
	}
	ids := make(map[string]struct{}, len(limits.Concurrency))
	for _, item := range limits.Concurrency {
		item.ID = strings.TrimSpace(item.ID)
		if !labelPattern.MatchString(item.ID) {
			return ExecutionLimits{}, fmt.Errorf("concurrency limit id %q is invalid", item.ID)
		}
		if _, exists := ids[item.ID]; exists {
			return ExecutionLimits{}, fmt.Errorf("concurrency limit id %q is duplicated", item.ID)
		}
		ids[item.ID] = struct{}{}
		if item.MaxConcurrent <= 0 {
			return ExecutionLimits{}, fmt.Errorf("concurrency limit %q maxConcurrent must be positive", item.ID)
		}
		if len(item.InputPointers) == 0 || len(item.InputPointers) > MaxConcurrencyInputPointers {
			return ExecutionLimits{}, fmt.Errorf("concurrency limit %q inputPointers must contain between 1 and %d entries", item.ID, MaxConcurrencyInputPointers)
		}
		pointers := make([]string, 0, len(item.InputPointers))
		seenPointers := make(map[string]struct{}, len(item.InputPointers))
		for _, pointer := range item.InputPointers {
			if err := ValidateInputJSONPointer(pointer); err != nil {
				return ExecutionLimits{}, fmt.Errorf("concurrency limit %q input pointer: %w", item.ID, err)
			}
			if _, exists := seenPointers[pointer]; exists {
				return ExecutionLimits{}, fmt.Errorf("concurrency limit %q input pointer %q is duplicated", item.ID, pointer)
			}
			seenPointers[pointer] = struct{}{}
			pointers = append(pointers, pointer)
		}
		item.InputPointers = pointers
		normalized.Concurrency = append(normalized.Concurrency, item)
	}
	if len(normalized.Concurrency) == 0 {
		normalized.Concurrency = nil
	}
	rateIDs := make(map[string]struct{}, len(limits.Rate))
	for _, item := range limits.Rate {
		item.ID = strings.TrimSpace(item.ID)
		if !labelPattern.MatchString(item.ID) {
			return ExecutionLimits{}, fmt.Errorf("rate limit id %q is invalid", item.ID)
		}
		if _, exists := rateIDs[item.ID]; exists {
			return ExecutionLimits{}, fmt.Errorf("rate limit id %q is duplicated", item.ID)
		}
		rateIDs[item.ID] = struct{}{}
		if item.MaxAttempts <= 0 {
			return ExecutionLimits{}, fmt.Errorf("rate limit %q maxAttempts must be positive", item.ID)
		}
		if item.WindowSeconds < MinRateWindowSeconds || item.WindowSeconds > MaxRateWindowSeconds {
			return ExecutionLimits{}, fmt.Errorf("rate limit %q windowSeconds must be between %d and %d", item.ID, MinRateWindowSeconds, MaxRateWindowSeconds)
		}
		if len(item.InputPointers) == 0 || len(item.InputPointers) > MaxExecutionInputPointers {
			return ExecutionLimits{}, fmt.Errorf("rate limit %q inputPointers must contain between 1 and %d entries", item.ID, MaxExecutionInputPointers)
		}
		pointers := make([]string, 0, len(item.InputPointers))
		seenPointers := make(map[string]struct{}, len(item.InputPointers))
		for _, pointer := range item.InputPointers {
			if err := ValidateInputJSONPointer(pointer); err != nil {
				return ExecutionLimits{}, fmt.Errorf("rate limit %q input pointer: %w", item.ID, err)
			}
			if _, exists := seenPointers[pointer]; exists {
				return ExecutionLimits{}, fmt.Errorf("rate limit %q input pointer %q is duplicated", item.ID, pointer)
			}
			seenPointers[pointer] = struct{}{}
			pointers = append(pointers, pointer)
		}
		item.InputPointers = pointers
		normalized.Rate = append(normalized.Rate, item)
	}
	if len(normalized.Rate) == 0 {
		normalized.Rate = nil
	}
	return normalized, nil
}

// ValidateInputJSONPointer validates the bounded RFC 6901 subset accepted by
// execution limits. Resolution supports object members and array indices.
func ValidateInputJSONPointer(pointer string) error {
	if pointer == "" || !strings.HasPrefix(pointer, "/") {
		return fmt.Errorf("%q must be a non-empty RFC 6901 pointer", pointer)
	}
	if len(pointer) > MaxConcurrencyPointerBytes || !utf8.ValidString(pointer) {
		return fmt.Errorf("%q exceeds the UTF-8 pointer limit", pointer)
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		for index := 0; index < len(token); index++ {
			if token[index] != '~' {
				continue
			}
			if index+1 >= len(token) || (token[index+1] != '0' && token[index+1] != '1') {
				return fmt.Errorf("%q contains an invalid escape", pointer)
			}
			index++
		}
	}
	return nil
}

// RuntimeAccess is the release-owned allowlist for Action SDK lookups.
// Admission augments it with references discovered in the effective input and
// pins the result on the Job. Values are never pinned here.
type RuntimeAccess struct {
	Variables []string `json:"variables,omitempty"`
	Resources []string `json:"resources,omitempty"`
}

func NormalizeRuntimeAccess(access RuntimeAccess) (RuntimeAccess, error) {
	variables, err := normalizeRuntimeConfigPaths(access.Variables)
	if err != nil {
		return RuntimeAccess{}, fmt.Errorf("variables: %w", err)
	}
	resources, err := normalizeRuntimeConfigPaths(access.Resources)
	if err != nil {
		return RuntimeAccess{}, fmt.Errorf("resources: %w", err)
	}
	if len(variables)+len(resources) > 256 {
		return RuntimeAccess{}, fmt.Errorf("runtime access exceeds 256 paths")
	}
	return RuntimeAccess{Variables: variables, Resources: resources}, nil
}

func NormalizeRuntimeConfigPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || !utf8.ValidString(value) || strings.Contains(value, `\`) {
		return "", fmt.Errorf("invalid runtime configuration path %q", value)
	}
	segments := strings.Split(value, "/")
	if len(segments) > 32 {
		return "", fmt.Errorf("runtime configuration path %q exceeds 32 segments", value)
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid runtime configuration path %q", value)
		}
		for _, item := range segment {
			if !validPortableKeyRune(item) && item != '.' && item != '-' {
				return "", fmt.Errorf("invalid runtime configuration path %q", value)
			}
		}
	}
	return value, nil
}

func normalizeRuntimeConfigPaths(values []string) ([]string, error) {
	unique := map[string]struct{}{}
	for _, value := range values {
		normalized, err := NormalizeRuntimeConfigPath(value)
		if err != nil {
			return nil, err
		}
		unique[normalized] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

// ActionAdapter is reserved for runtime integrations outside source manifests.
// Source windforce.json files use the app-level ctx-first entrypoint runner.
type ActionAdapter struct {
	Type    string                     `json:"type,omitempty"`
	Command []string                   `json:"command,omitempty"`
	Env     []string                   `json:"env,omitempty"`
	Options map[string]json.RawMessage `json:"options,omitempty"`
}

// Deployment is the active source bundle selected by the catalog.
type Deployment struct {
	Workspace              string            `json:"workspace,omitempty"`
	GitSourceID            string            `json:"gitSourceId,omitempty"`
	App                    string            `json:"app"`
	Version                string            `json:"version,omitempty"`
	Tag                    string            `json:"tag,omitempty"`
	TagOverride            *string           `json:"tagOverride,omitempty"`
	RequiredLabelsOverride *[]string         `json:"requiredLabelsOverride,omitempty"`
	Entrypoint             string            `json:"entrypoint,omitempty"`
	Runtime                string            `json:"runtime,omitempty"`
	ScriptLang             string            `json:"scriptLang,omitempty"`
	TimeoutS               int32             `json:"timeout,omitempty"`
	MaxConcurrent          *int32            `json:"maxConcurrent,omitempty"`
	ExecutionLimits        ExecutionLimits   `json:"executionLimits,omitempty,omitzero"`
	RequiredCapabilities   []string          `json:"requiredCapabilities,omitempty"`
	RequiredLabels         []string          `json:"requiredLabels,omitempty"`
	Commit                 string            `json:"commit"`
	Message                *string           `json:"message,omitempty"`
	Source                 string            `json:"source,omitempty"`
	DeploymentID           *string           `json:"deploymentId,omitempty"`
	CreatedBy              *string           `json:"createdBy,omitempty"`
	BundleDigest           string            `json:"bundleDigest,omitempty"`
	BundleURI              string            `json:"bundleUri,omitempty"`
	ExecutionProfile       ExecutionProfile  `json:"executionProfile,omitempty,omitzero"`
	ObjectURI              string            `json:"objectUri"`
	Actions                map[string]Action `json:"actions"`
	UpdatedAt              *time.Time        `json:"updatedAt,omitempty"`
}

// PinExecutionDeployment keeps only the selected action while preserving the
// release coordinates and defaults required to retry the same execution.
func PinExecutionDeployment(deployment Deployment, actionKey string) Deployment {
	pinned := deployment
	pinned.ExecutionLimits = cloneExecutionLimits(deployment.ExecutionLimits)
	pinned.RequiredCapabilities = append([]string(nil), deployment.RequiredCapabilities...)
	pinned.RequiredLabels = append([]string(nil), deployment.RequiredLabels...)
	if deployment.RequiredLabelsOverride != nil {
		cloned := append([]string{}, (*deployment.RequiredLabelsOverride)...)
		pinned.RequiredLabelsOverride = &cloned
	}
	pinned.Actions = make(map[string]Action, 1)
	if action, ok := deployment.Actions[actionKey]; ok {
		action.Command = append([]string(nil), action.Command...)
		action.RuntimeAccess.Variables = append([]string(nil), action.RuntimeAccess.Variables...)
		action.RuntimeAccess.Resources = append([]string(nil), action.RuntimeAccess.Resources...)
		action.InputSchemaBody = append(json.RawMessage(nil), action.InputSchemaBody...)
		action.OutputSchemaBody = append(json.RawMessage(nil), action.OutputSchemaBody...)
		action.OperatorSettingsSchemaBody = append(json.RawMessage(nil), action.OperatorSettingsSchemaBody...)
		action.ExecutionLimits = cloneExecutionLimits(action.ExecutionLimits)
		if action.RequiredLabelsOverride != nil {
			cloned := append([]string{}, (*action.RequiredLabelsOverride)...)
			action.RequiredLabelsOverride = &cloned
		}
		pinned.Actions[actionKey] = action
	}
	return pinned
}

func cloneExecutionLimits(limits ExecutionLimits) ExecutionLimits {
	cloned := ExecutionLimits{
		Concurrency: make([]KeyedConcurrencyLimit, len(limits.Concurrency)),
		Rate:        make([]KeyedRateLimit, len(limits.Rate)),
	}
	for index, limit := range limits.Concurrency {
		cloned.Concurrency[index] = limit
		cloned.Concurrency[index].InputPointers = append([]string(nil), limit.InputPointers...)
	}
	for index, limit := range limits.Rate {
		cloned.Rate[index] = limit
		cloned.Rate[index].InputPointers = append([]string(nil), limit.InputPointers...)
	}
	if len(cloned.Concurrency) == 0 {
		cloned.Concurrency = nil
	}
	if len(cloned.Rate) == 0 {
		cloned.Rate = nil
	}
	return cloned
}

// JobRequest is the runtime request passed into windforce-core.
type JobRequest struct {
	JobID      string          `json:"jobId"`
	App        string          `json:"app"`
	Action     string          `json:"action"`
	Input      json.RawMessage `json:"input"`
	Deployment Deployment      `json:"deployment"`
}

// JobResult is the subprocess execution result as observed by the runtime.
type JobResult struct {
	JobID      string          `json:"jobId,omitempty"`
	App        string          `json:"app"`
	Action     string          `json:"action"`
	Output     json.RawMessage `json:"output,omitempty"`
	ExitCode   int             `json:"exitCode"`
	Stdout     string          `json:"stdout,omitempty"`
	Stderr     string          `json:"stderr,omitempty"`
	DurationMs int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
	// Interruption identifies infrastructure or operator control that stopped
	// execution. Action failures remain represented by Error and ExitCode.
	Interruption *ExecutionInterruption `json:"interruption,omitempty"`
}

const (
	InterruptionActionTimeout  = "action_timeout"
	InterruptionRunCanceled    = "run_canceled"
	InterruptionLeaseLost      = "lease_lost"
	InterruptionWorkerShutdown = "worker_shutdown"
)

// ExecutionInterruption is a stable, non-secret explanation for why Core
// stopped an Action. It is safe to persist with the public Run result.
type ExecutionInterruption struct {
	Cause    string    `json:"cause"`
	Source   string    `json:"source"`
	Message  string    `json:"message,omitempty"`
	Observed time.Time `json:"observedAt"`
}

func (a Action) AdapterType() string {
	if a.Adapter == nil {
		return ActionAdapterJSONFile
	}
	value := strings.TrimSpace(a.Adapter.Type)
	if value == "" {
		return ActionAdapterJSONFile
	}
	return value
}

func EffectiveRouteTag(appTag string, appTagOverride *string, actionTag *string, actionTagOverride *string) string {
	if actionTagOverride != nil && strings.TrimSpace(*actionTagOverride) != "" {
		return strings.TrimSpace(*actionTagOverride)
	}
	if appTagOverride != nil && strings.TrimSpace(*appTagOverride) != "" {
		return strings.TrimSpace(*appTagOverride)
	}
	if actionTag != nil && strings.TrimSpace(*actionTag) != "" {
		return strings.TrimSpace(*actionTag)
	}
	if strings.TrimSpace(appTag) != "" {
		return strings.TrimSpace(appTag)
	}
	return DefaultRouteTag
}

// NormalizeLabels validates and canonicalizes a worker label set: lowercase
// tokens matching the label pattern, at most MaxLabels, deduplicated and
// sorted. Reserved sys/ labels are rejected unless allowReserved (worker
// startup configuration, which the operator owns).
func NormalizeLabels(labels []string, allowReserved bool) ([]string, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			return nil, fmt.Errorf("label must not be empty")
		}
		body, reserved := strings.CutPrefix(label, ReservedLabelPrefix)
		if reserved && !allowReserved {
			return nil, fmt.Errorf("label %q uses the reserved %s prefix", label, ReservedLabelPrefix)
		}
		if !labelPattern.MatchString(body) {
			return nil, fmt.Errorf("invalid label %q", label)
		}
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	if len(out) > MaxLabels {
		return nil, fmt.Errorf("at most %d labels are allowed", MaxLabels)
	}
	sort.Strings(out)
	return out, nil
}

// NormalizeCapabilities is the requiredCapabilities alias of NormalizeLabels:
// the vocabulary is open, capabilities are labels by their manifest name.
func NormalizeCapabilities(caps []string) ([]string, error) {
	normalized, err := NormalizeLabels(caps, false)
	if err != nil {
		return nil, fmt.Errorf("capability: %w", err)
	}
	return normalized, nil
}

// EffectiveRequiredLabels resolves the label set pinned onto a job: the
// deployment (app-level) labels unioned with the action's contribution.
// Legacy deployments that only carry requiredCapabilities are honored.
func EffectiveRequiredLabels(deployment Deployment, action Action) []string {
	var effective []string
	if action.RequiredLabelsOverride != nil {
		effective = normalizedEffectiveLabels(*action.RequiredLabelsOverride)
	} else if deployment.RequiredLabelsOverride != nil {
		effective = normalizedEffectiveLabels(*deployment.RequiredLabelsOverride)
	} else {
		base := deployment.RequiredLabels
		if base == nil {
			base = deployment.RequiredCapabilities
		}
		merged := append([]string(nil), base...)
		if action.RunsOn != nil {
			merged = append(merged, *action.RunsOn...)
		} else if action.Capabilities != nil {
			merged = append(merged, *action.Capabilities...)
		}
		effective = normalizedEffectiveLabels(merged)
	}
	if strings.TrimSpace(deployment.ExecutionProfile.Key) == "" {
		return effective
	}
	withProfile, err := WithExecutionProfileLabel(effective, deployment.ExecutionProfile)
	if err != nil {
		// A persisted invalid profile is handled by bundle validation. Placement
		// must not silently replace the already pinned label set.
		return effective
	}
	return normalizedEffectiveLabels(withProfile)
}

func normalizedEffectiveLabels(labels []string) []string {
	if len(labels) == 0 {
		if labels != nil {
			return []string{}
		}
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// EffectiveRouteTagForApp resolves the app's explicit release-routing tag.
// Labels do not influence route tags (ADR 0009: tags and labels are
// orthogonal claim dimensions).
func EffectiveRouteTagForApp(deployment Deployment) string {
	return EffectiveRouteTag(deployment.Tag, deployment.TagOverride, nil, nil)
}

// EffectiveRouteTagForAction resolves the action's explicit release-routing
// tag; labels are matched separately at claim time.
func EffectiveRouteTagForAction(deployment Deployment, action Action) string {
	return EffectiveRouteTag(deployment.Tag, deployment.TagOverride, action.Tag, action.TagOverride)
}

func NormalizeWorkspace(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultWorkspace
	}
	return value
}

func ValidWorkspaceID(value string) bool {
	if len(value) < 2 || len(value) > 48 || !utf8.ValidString(value) {
		return false
	}
	for index, item := range value {
		if item >= 'a' && item <= 'z' || item >= '0' && item <= '9' {
			continue
		}
		if item == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return value[0] >= 'a' && value[0] <= 'z'
}

func NormalizeGitSourceID(value string, app string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	app = strings.TrimSpace(app)
	if app != "" {
		return app
	}
	return DefaultGitSourceID
}

func ValidAppKey(value string) bool {
	if len(value) < 2 || len(value) > 64 || !utf8.ValidString(value) {
		return false
	}
	for _, item := range value {
		if !validPortableKeyRune(item) {
			return false
		}
	}
	return true
}

func ValidActionKey(value string) bool {
	if value == "" || len(value) > 128 || !utf8.ValidString(value) {
		return false
	}
	if strings.ContainsAny(value, `/\`) {
		return false
	}
	segments := strings.Split(value, ".")
	if len(segments) > 8 {
		return false
	}
	for _, segment := range segments {
		if segment == "" {
			return false
		}
		for _, item := range segment {
			if !validPortableKeyRune(item) {
				return false
			}
		}
	}
	return true
}

func validPortableKeyRune(item rune) bool {
	if item >= 'a' && item <= 'z' {
		return true
	}
	if item >= 'A' && item <= 'Z' {
		return true
	}
	if item >= '0' && item <= '9' {
		return true
	}
	return item == '_'
}

func NormalizeSourcePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" || value == "." {
		return "", nil
	}
	clean := path.Clean(value)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", fmt.Errorf("source path %q must be a relative path inside the git source", value)
	}
	return clean, nil
}

func ValidateSourceSubpath(value string) error {
	if value == "" {
		return nil
	}
	if filepath.IsAbs(value) || path.IsAbs(value) || strings.Contains(value, "..") {
		return fmt.Errorf("source path %q must be a relative path inside the git source", value)
	}
	return nil
}

func (d Deployment) SourceWorkspace() string {
	return NormalizeWorkspace(d.Workspace)
}

func (d Deployment) SourceGitSourceID() string {
	return NormalizeGitSourceID(d.GitSourceID, d.App)
}

func (d Deployment) SourceObjectURI() string {
	return fmt.Sprintf("bundle://%s/%s/%s", d.SourceWorkspace(), d.SourceGitSourceID(), d.Commit)
}
