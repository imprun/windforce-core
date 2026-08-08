package manifest

import (
	"encoding/json"
	"fmt"
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
	var parsed struct {
		contract.App
		Flows map[string]json.RawMessage `json:"flows"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
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
		clearRuntimeOwnedActionManifestFields(&action)
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
