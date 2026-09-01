package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

// FileName is the manifest an app declares at its source root by default.
//
// A deployment may serve an app ecosystem that does not present itself as
// Windforce. Such an operator names the file after that ecosystem instead, once
// per instance, and every source is then read with that name. Only the name
// changes; the contents, validation, and every downstream stage are identical.
const FileName = "windforce.json"

// ResolveFileName returns the manifest name to read, falling back to FileName
// when the operator did not choose one.
func ResolveFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return FileName
	}
	return name
}

// Load reads the manifest named fileName from dir. An empty fileName reads the
// default.
func Load(dir string, fileName string) (contract.App, error) {
	fileName = ResolveFileName(fileName)
	path := filepath.Join(dir, fileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return contract.App{}, fmt.Errorf("no %s manifest at source root (subpath)", fileName)
		}
		return contract.App{}, err
	}
	return parseNamed(data, fileName)
}

// Parse reads manifest bytes that carry no file name of their own, so its
// diagnostics name the default. Load names the file the operator chose.
func Parse(data []byte) (contract.App, error) {
	return parseNamed(data, FileName)
}

func parseNamed(data []byte, fileName string) (contract.App, error) {
	fileName = ResolveFileName(fileName)
	parsed, err := decodeManifestDocument(data)
	if err != nil {
		return contract.App{}, fmt.Errorf("parse %s: %w", fileName, err)
	}
	app := parsed.App
	if !contract.ValidAppKey(app.App) {
		return contract.App{}, fmt.Errorf("invalid app key %q in %s", app.App, fileName)
	}
	if len(parsed.Flows) > 0 {
		return contract.App{}, fmt.Errorf("app %s declares flows in %s, but windforce-core does not support flows", app.App, fileName)
	}
	app.Runtime = ""
	if len(app.Actions) == 0 {
		return contract.App{}, fmt.Errorf("%s declares no actions", fileName)
	}
	if app.Entrypoint != "" && (filepath.IsAbs(app.Entrypoint) || strings.HasPrefix(app.Entrypoint, "/") || strings.Contains(app.Entrypoint, "..")) {
		return contract.App{}, fmt.Errorf("app %s entrypoint %q must be a relative path inside the app", app.App, app.Entrypoint)
	}
	scriptLang, err := contract.NormalizeScriptLanguage(app.ScriptLang)
	if err != nil {
		return contract.App{}, fmt.Errorf("app %s: %w", app.App, err)
	}
	app.ScriptLang = scriptLang
	if app.TimeoutS == 0 {
		app.TimeoutS = contract.DefaultTimeoutS
	}
	if app.MaxConcurrent != nil && *app.MaxConcurrent <= 0 {
		return contract.App{}, fmt.Errorf("app %s maxConcurrent must be positive in %s", app.App, fileName)
	}
	app.ExecutionLimits, err = contract.NormalizeExecutionLimits(app.ExecutionLimits)
	if err != nil {
		return contract.App{}, fmt.Errorf("app %s executionLimits: %w", app.App, err)
	}
	caps, err := contract.NormalizeCapabilities(app.Capabilities)
	if err != nil {
		return contract.App{}, fmt.Errorf("app %s capabilities: %w", app.App, err)
	}
	app.Capabilities = caps
	runsOn, err := contract.NormalizeLabels(append(append([]string{}, app.RunsOn...), app.Capabilities...), false)
	if err != nil {
		return contract.App{}, fmt.Errorf("app %s runsOn: %w", app.App, err)
	}
	app.RunsOn = runsOn

	for name, action := range app.Actions {
		if !contract.ValidActionKey(name) {
			return contract.App{}, fmt.Errorf("invalid action key %q in %s", name, fileName)
		}
		action.Action = name
		if action.Adapter != nil {
			return contract.App{}, fmt.Errorf("action %s.%s adapter is not supported in %s", app.App, name, fileName)
		}
		if len(action.Command) > 0 {
			return contract.App{}, fmt.Errorf("action %s.%s command is not supported in %s", app.App, name, fileName)
		}
		if strings.TrimSpace(action.Runtime) != "" {
			return contract.App{}, fmt.Errorf("action %s.%s runtime is not supported in %s; set app scriptLang once for the Release", app.App, name, fileName)
		}
		clearRuntimeOwnedActionManifestFields(&action)
		if len(action.PublicInterfaces) > 0 && app.APIVersion != contract.AppManifestV2 {
			return contract.App{}, fmt.Errorf("action %s.%s publicInterfaces requires apiVersion %q in %s", app.App, name, contract.AppManifestV2, fileName)
		}
		action.PublicInterfaces, err = contract.NormalizePublicInterfaces(action.PublicInterfaces)
		if err != nil {
			return contract.App{}, fmt.Errorf("action %s.%s: %w", app.App, name, err)
		}
		action.ExecutionLimits, err = contract.NormalizeExecutionLimits(action.ExecutionLimits)
		if err != nil {
			return contract.App{}, fmt.Errorf("action %s.%s executionLimits: %w", app.App, name, err)
		}
		if action.Capabilities != nil {
			caps, err := contract.NormalizeCapabilities(*action.Capabilities)
			if err != nil {
				return contract.App{}, fmt.Errorf("action %s.%s capabilities: %w", app.App, name, err)
			}
			if caps == nil {
				caps = []string{}
			}
			action.Capabilities = &caps
		}
		if action.RunsOn != nil || action.Capabilities != nil {
			merged := []string{}
			if action.RunsOn != nil {
				merged = append(merged, *action.RunsOn...)
			}
			if action.Capabilities != nil {
				merged = append(merged, *action.Capabilities...)
			}
			labels, err := contract.NormalizeLabels(merged, false)
			if err != nil {
				return contract.App{}, fmt.Errorf("action %s.%s runsOn: %w", app.App, name, err)
			}
			if labels == nil {
				labels = []string{}
			}
			action.RunsOn = &labels
			// Claim-time pinning unions app and action labels; a union no
			// worker can offer (labels are capped) must fail here, not sit
			// queued forever.
			if _, err := contract.NormalizeLabels(append(append([]string{}, app.RunsOn...), labels...), false); err != nil {
				return contract.App{}, fmt.Errorf("action %s.%s runsOn combined with app runsOn: %w", app.App, name, err)
			}
		}
		applyAppDefaults(app, &action)
		action.RuntimeAccess, err = contract.NormalizeRuntimeAccess(action.RuntimeAccess)
		if err != nil {
			return contract.App{}, fmt.Errorf("action %s.%s runtimeAccess: %w", app.App, name, err)
		}
		if err := validateExecutableAction(app.App, name, action, fileName); err != nil {
			return contract.App{}, err
		}
		app.Actions[name] = action
	}
	if app.Tag == "" {
		app.Tag = contract.DefaultRouteTag
	}
	return app, nil
}

type manifestDocument struct {
	contract.App
	Flows map[string]json.RawMessage `json:"flows"`
}

// manifestV2Document is the source-authored v2 allowlist. Keep runtime-owned
// Deployment and Action fields out of this type so strict decoding cannot
// accept a value that parseNamed would later clear or overwrite.
type manifestV2Document struct {
	APIVersion      string                      `json:"apiVersion"`
	App             string                      `json:"app"`
	Name            string                      `json:"name,omitempty"`
	Entrypoint      string                      `json:"entrypoint,omitempty"`
	ScriptLang      string                      `json:"scriptLang,omitempty"`
	TimeoutS        int32                       `json:"timeout,omitempty"`
	Tag             string                      `json:"tag,omitempty"`
	MaxConcurrent   *int32                      `json:"maxConcurrent,omitempty"`
	ExecutionLimits contract.ExecutionLimits    `json:"executionLimits,omitempty,omitzero"`
	Capabilities    []string                    `json:"capabilities,omitempty"`
	RunsOn          []string                    `json:"runsOn,omitempty"`
	Actions         map[string]manifestV2Action `json:"actions"`
}

type manifestV2Action struct {
	Tag                    *string                  `json:"tag,omitempty"`
	Entrypoint             string                   `json:"entrypoint,omitempty"`
	InputSchema            string                   `json:"inputSchema,omitempty"`
	OutputSchema           string                   `json:"outputSchema,omitempty"`
	OperatorSettingsSchema string                   `json:"operatorSettingsSchema,omitempty"`
	PublicInterfaces       json.RawMessage          `json:"publicInterfaces,omitempty"`
	TimeoutS               *int32                   `json:"timeout,omitempty"`
	Capabilities           *[]string                `json:"capabilities,omitempty"`
	RunsOn                 *[]string                `json:"runsOn,omitempty"`
	RuntimeAccess          contract.RuntimeAccess   `json:"runtimeAccess,omitempty"`
	ExecutionLimits        contract.ExecutionLimits `json:"executionLimits,omitempty,omitzero"`
}

func decodeManifestDocument(data []byte) (manifestDocument, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return manifestDocument{}, err
	}
	strict := false
	if rawVersion, declared := root["apiVersion"]; declared {
		var version string
		if err := json.Unmarshal(rawVersion, &version); err != nil {
			return manifestDocument{}, errors.New("apiVersion must be a string")
		}
		if version != contract.AppManifestV2 {
			return manifestDocument{}, fmt.Errorf("unsupported apiVersion %q", version)
		}
		strict = true
	}
	if strict {
		return decodeManifestV2Document(data)
	}
	if err := rejectV1PublicInterfaces(root); err != nil {
		return manifestDocument{}, err
	}
	var parsed manifestDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&parsed); err != nil {
		return manifestDocument{}, err
	}
	if err := ensureManifestEOF(decoder); err != nil {
		return manifestDocument{}, err
	}
	return parsed, nil
}

func decodeManifestV2Document(data []byte) (manifestDocument, error) {
	var source manifestV2Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&source); err != nil {
		return manifestDocument{}, err
	}
	if err := ensureManifestEOF(decoder); err != nil {
		return manifestDocument{}, err
	}
	actions := make(map[string]contract.Action, len(source.Actions))
	for name, sourceAction := range source.Actions {
		publicInterfaces, err := decodeV2PublicInterfaces(sourceAction.PublicInterfaces)
		if err != nil {
			return manifestDocument{}, fmt.Errorf("action %s publicInterfaces: %w", name, err)
		}
		actions[name] = contract.Action{
			Tag:                    sourceAction.Tag,
			Entrypoint:             sourceAction.Entrypoint,
			InputSchema:            sourceAction.InputSchema,
			OutputSchema:           sourceAction.OutputSchema,
			OperatorSettingsSchema: sourceAction.OperatorSettingsSchema,
			PublicInterfaces:       publicInterfaces,
			TimeoutS:               sourceAction.TimeoutS,
			Capabilities:           sourceAction.Capabilities,
			RunsOn:                 sourceAction.RunsOn,
			RuntimeAccess:          sourceAction.RuntimeAccess,
			ExecutionLimits:        sourceAction.ExecutionLimits,
		}
	}
	return manifestDocument{
		App: contract.App{
			APIVersion:      source.APIVersion,
			App:             source.App,
			Name:            source.Name,
			Entrypoint:      source.Entrypoint,
			ScriptLang:      source.ScriptLang,
			TimeoutS:        source.TimeoutS,
			Tag:             source.Tag,
			MaxConcurrent:   source.MaxConcurrent,
			ExecutionLimits: source.ExecutionLimits,
			Capabilities:    source.Capabilities,
			RunsOn:          source.RunsOn,
			Actions:         actions,
		},
	}, nil
}

func decodeV2PublicInterfaces(raw json.RawMessage) ([]json.RawMessage, error) {
	if raw == nil {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("must be an array, not null")
	}
	var declarations []json.RawMessage
	if err := json.Unmarshal(raw, &declarations); err != nil {
		return nil, fmt.Errorf("must be an array: %w", err)
	}
	return declarations, nil
}

func rejectV1PublicInterfaces(root map[string]json.RawMessage) error {
	rawActions, ok := root["actions"]
	if !ok {
		return nil
	}
	var actions map[string]json.RawMessage
	if err := json.Unmarshal(rawActions, &actions); err != nil {
		return err
	}
	for name, rawAction := range actions {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawAction, &fields); err != nil {
			return err
		}
		if _, declared := fields["publicInterfaces"]; declared {
			return fmt.Errorf("action %s publicInterfaces requires apiVersion %q", name, contract.AppManifestV2)
		}
	}
	return nil
}

func ensureManifestEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("manifest has trailing JSON values")
}

func clearRuntimeOwnedActionManifestFields(action *contract.Action) {
	action.TagOverride = nil
	action.InputSchemaBody = nil
	action.OutputSchemaBody = nil
	action.OperatorSettingsSchemaBody = nil
	action.UpdatedAt = nil
}

func applyAppDefaults(app contract.App, action *contract.Action) {
	if action.Entrypoint == "" {
		action.Entrypoint = app.Entrypoint
	}
	if action.Runtime == "" {
		action.Runtime = app.ScriptLang
	}
	if action.TimeoutMs == 0 {
		if action.TimeoutS != nil {
			action.TimeoutMs = int64(*action.TimeoutS) * 1000
		} else if app.TimeoutS > 0 {
			action.TimeoutMs = int64(app.TimeoutS) * 1000
		}
	}
}

func validateExecutableAction(app string, actionName string, action contract.Action, fileName string) error {
	if action.Adapter != nil {
		return fmt.Errorf("action %s.%s adapter is not supported in %s", app, actionName, fileName)
	}
	if len(action.Command) > 0 {
		return fmt.Errorf("action %s.%s command is not supported in %s", app, actionName, fileName)
	}
	if action.Entrypoint == "" {
		return fmt.Errorf("app %s has no entrypoint in %s", app, fileName)
	}
	if err := validateActionPath(app, actionName, "entrypoint", action.Entrypoint); err != nil {
		return err
	}
	return nil
}

func validateActionPath(app string, action string, field string, value string) error {
	if value == "" {
		return nil
	}
	owner := fmt.Sprintf("action %s.%s", app, action)
	if action == "" {
		owner = "app " + app
	}
	if filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.Contains(value, "..") {
		return fmt.Errorf("%s %s path %q must be a relative path inside the app", owner, field, value)
	}
	return nil
}
