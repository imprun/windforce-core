import {
  CheckCircle2,
  Clipboard,
  Download,
  FileInput,
  Play,
  RotateCcw,
  ShieldCheck,
  Trash2,
  Upload,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Layout } from "../components/Layout";
import { SettingsNav } from "../components/SettingsNav";
import { EmptyState, ErrorNotice, Field, Panel, SelectControl } from "../components/ui";
import { errorMessage, type ProvisioningAppliedResource } from "../lib/api";
import { useApp } from "../lib/app-context";
import { translate } from "../shared/i18n";

const sampleYaml = `resources:
  - apiVersion: windforce-lite.imprun.dev/v1
    kind: AppSource
    metadata:
      name: example-app
    spec:
      repository:
        url: https://example.test/group/example-app.git
        branch: main
        authRef: example-app-git

  - apiVersion: windforce-lite.imprun.dev/v1
    kind: GitCredential
    metadata:
      name: example-app-git
    spec:
      method: pat
      storageRef: git/example-app/credential
      token:
        valueFrom:
          env: EXAMPLE_APP_GIT_TOKEN
`;

type ImportFormat = "yaml" | "json";
type ExportFormat = "yaml" | "json";
type ProvisioningTask = "import" | "export";

export function ProvisioningPage() {
  const { api, notify, settings } = useApp();
  const [task, setTask] = useState<ProvisioningTask>("import");
  const [exportFormat, setExportFormat] = useState<ExportFormat>("yaml");
  const [includeValues, setIncludeValues] = useState(false);
  const [exportText, setExportText] = useState("");
  const [exporting, setExporting] = useState(false);
  const [importFormat, setImportFormat] = useState<ImportFormat>("yaml");
  const [importText, setImportText] = useState(sampleYaml);
  const [dryRunResult, setDryRunResult] = useState<ProvisioningAppliedResource[]>([]);
  const [applyResult, setApplyResult] = useState<ProvisioningAppliedResource[]>([]);
  const [error, setError] = useState("");
  const [working, setWorking] = useState<"dry-run" | "apply" | "export" | "">("");

  const importReady = importText.trim().length > 0;
  const canApply = importReady && dryRunResult.length > 0 && working === "";
  const resultRows = applyResult.length ? applyResult : dryRunResult;
  const resultLabel = applyResult.length
    ? translate("provisioning.appliedResources")
    : translate("provisioning.dryRunResult");
  const resultSummary = summarizeResult(resultRows);
  const exportFileName = useMemo(
    () =>
      `windforce-lite-${settings.workspace || "default"}-provisioning.${exportFormat === "yaml" ? "yaml" : "json"}`,
    [exportFormat, settings.workspace],
  );

  async function handleExport() {
    setError("");
    setExporting(true);
    setWorking("export");
    try {
      const text = await api.exportProvisioning(exportFormat, includeValues);
      setExportText(text);
      notify("ok", translate("provisioning.exportRefreshed"));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setExporting(false);
      setWorking("");
    }
  }

  async function handleDryRun() {
    setError("");
    setApplyResult([]);
    setWorking("dry-run");
    try {
      const result = await api.importProvisioning(importText, true, importFormat);
      setDryRunResult(result.applied || []);
      notify("ok", translate("provisioning.dryRunCompleted"));
    } catch (cause) {
      setDryRunResult([]);
      setError(errorMessage(cause));
    } finally {
      setWorking("");
    }
  }

  async function handleApply() {
    setError("");
    setWorking("apply");
    try {
      const result = await api.importProvisioning(importText, false, importFormat);
      setApplyResult(result.applied || []);
      setDryRunResult([]);
      notify("ok", translate("provisioning.applied"));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setWorking("");
    }
  }

  async function copyExport() {
    if (!exportText) return;
    try {
      await navigator.clipboard.writeText(exportText);
      notify("ok", translate("provisioning.exportCopied"));
    } catch (cause) {
      setError(errorMessage(cause));
    }
  }

  function downloadExport() {
    if (!exportText) return;
    const blob = new Blob([exportText], {
      type: exportFormat === "yaml" ? "application/yaml" : "application/json",
    });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = exportFileName;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  async function handleFile(file: File | null) {
    if (!file) return;
    const text = await file.text();
    setImportText(text);
    const lower = file.name.toLowerCase();
    setImportFormat(lower.endsWith(".json") ? "json" : "yaml");
    setDryRunResult([]);
    setApplyResult([]);
    setError("");
  }

  function resetImportDocument(text: string, format: ImportFormat) {
    setImportText(text);
    setImportFormat(format);
    setDryRunResult([]);
    setApplyResult([]);
    setError("");
  }

  return (
    <Layout title={translate("navigation.settings")} subtitle={translate("provisioning.subtitle")}>
      <SettingsNav />
      {error ? <ErrorNotice message={error} /> : null}

      <div
        className="tabBar provisioningModeTabs"
        role="tablist"
        aria-label={translate("provisioning.mode")}
      >
        <button
          type="button"
          role="tab"
          aria-selected={task === "import"}
          className={task === "import" ? "tab active" : "tab"}
          onClick={() => setTask("import")}
        >
          {translate("provisioning.import")}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={task === "export"}
          className={task === "export" ? "tab active" : "tab"}
          onClick={() => setTask("export")}
        >
          {translate("provisioning.export")}
        </button>
      </div>

      {task === "import" ? (
        <section
          className="provisioningWorkspace"
          aria-label={translate("provisioning.importDocument")}
        >
          <Panel
            title={translate("provisioning.importDocument")}
            subtitle={translate("provisioning.importDocumentHint")}
          >
            <div className="provisioningDocumentHeader">
              <Field label={translate("provisioning.format")}>
                <SelectControl
                  ariaLabel={translate("provisioning.importFormat")}
                  value={importFormat}
                  onChange={(value) => {
                    setImportFormat(value);
                    setDryRunResult([]);
                    setApplyResult([]);
                  }}
                  options={[
                    { value: "yaml", label: "YAML" },
                    { value: "json", label: "JSON" },
                  ]}
                />
              </Field>
              <label className="button">
                <FileInput aria-hidden="true" />
                {translate("provisioning.loadFile")}
                <input
                  className="visuallyHidden"
                  type="file"
                  accept=".yaml,.yml,.json,application/json,application/yaml,text/yaml"
                  onChange={(event) => void handleFile(event.target.files?.[0] || null)}
                />
              </label>
              <button
                className="button"
                type="button"
                onClick={() => resetImportDocument(sampleYaml, "yaml")}
              >
                <RotateCcw aria-hidden="true" />
                {translate("provisioning.resetSample")}
              </button>
            </div>
            <Field label={translate("provisioning.document")}>
              <textarea
                className="provisioningEditor"
                value={importText}
                spellCheck={false}
                onChange={(event) => {
                  setImportText(event.target.value);
                  setDryRunResult([]);
                  setApplyResult([]);
                }}
              />
            </Field>
          </Panel>

          <aside
            className="provisioningSidePanel"
            aria-label={translate("provisioning.importControls")}
          >
            <Panel
              title={translate("provisioning.reviewApply")}
              subtitle={translate("provisioning.reviewApplyHint")}
            >
              <div className="provisioningActionStack">
                <button
                  className="button"
                  type="button"
                  disabled={!importReady || working !== ""}
                  onClick={handleDryRun}
                >
                  <Play aria-hidden="true" />
                  {working === "dry-run"
                    ? translate("repository.checking")
                    : translate("provisioning.dryRun")}
                </button>
                <button
                  className="button primary"
                  type="button"
                  disabled={!canApply}
                  onClick={handleApply}
                >
                  <Upload aria-hidden="true" />
                  {working === "apply"
                    ? translate("provisioning.applying")
                    : translate("common.apply")}
                </button>
                <button
                  className="button"
                  type="button"
                  disabled={!importText || working !== ""}
                  onClick={() => resetImportDocument("", importFormat)}
                >
                  <Trash2 aria-hidden="true" />
                  {translate("provisioning.clearDocument")}
                </button>
              </div>
              <div className="provisioningSafety">
                <ShieldCheck aria-hidden="true" />
                <span>{translate("provisioning.dryRunSafety")}</span>
              </div>
            </Panel>

            <Panel
              title={resultLabel}
              subtitle={resultRows.length ? resultSummary : translate("provisioning.runDryRunHint")}
            >
              {resultRows.length ? (
                <ProvisioningResultList rows={resultRows} />
              ) : (
                <EmptyState title={translate("provisioning.noValidation")}>
                  <span>{translate("provisioning.noValidationHint")}</span>
                </EmptyState>
              )}
            </Panel>
          </aside>
        </section>
      ) : (
        <section
          className="provisioningWorkspace"
          aria-label={translate("provisioning.exportSnapshot")}
        >
          <Panel
            title={translate("provisioning.exportPreview")}
            subtitle={translate("provisioning.exportPreviewHint")}
          >
            {exportText ? (
              <>
                <div className="provisioningToolbar">
                  <span className="cellSub">{exportFileName}</span>
                </div>
                <pre className="provisioningCode">{exportText}</pre>
              </>
            ) : (
              <EmptyState title={translate("provisioning.noSnapshot")}>
                <span>{translate("provisioning.noSnapshotHint")}</span>
              </EmptyState>
            )}
          </Panel>

          <aside
            className="provisioningSidePanel"
            aria-label={translate("provisioning.exportControls")}
          >
            <Panel
              title={translate("provisioning.snapshotOptions")}
              subtitle={translate("provisioning.snapshotOptionsHint")}
            >
              <div className="formStack">
                <Field label={translate("provisioning.format")}>
                  <SelectControl
                    ariaLabel={translate("provisioning.exportFormat")}
                    value={exportFormat}
                    onChange={setExportFormat}
                    options={[
                      { value: "yaml", label: "YAML" },
                      { value: "json", label: "JSON" },
                    ]}
                  />
                </Field>
                <label className="toggleField">
                  <input
                    type="checkbox"
                    checked={includeValues}
                    onChange={(event) => setIncludeValues(event.target.checked)}
                  />
                  <span>
                    {translate("provisioning.includeValues")}
                    <small>{translate("provisioning.includeValuesHint")}</small>
                  </span>
                </label>
              </div>
              <div className="provisioningActionStack">
                <button
                  className="button primary"
                  type="button"
                  onClick={handleExport}
                  disabled={working === "export"}
                >
                  <Download aria-hidden="true" />
                  {exporting
                    ? translate("provisioning.exporting")
                    : translate("provisioning.exportSnapshotAction")}
                </button>
                <button
                  className="button"
                  type="button"
                  onClick={copyExport}
                  disabled={!exportText}
                >
                  <Clipboard aria-hidden="true" />
                  {translate("provisioning.copy")}
                </button>
                <button
                  className="button"
                  type="button"
                  onClick={downloadExport}
                  disabled={!exportText}
                >
                  <Download aria-hidden="true" />
                  {translate("provisioning.download")}
                </button>
              </div>
            </Panel>
          </aside>
        </section>
      )}
    </Layout>
  );
}

function ProvisioningResultList({ rows }: { rows: ProvisioningAppliedResource[] }) {
  return (
    <div className="provisioningResultList">
      {rows.map((row, index) => (
        <div className="provisioningResultItem" key={`${row.kind}-${row.name}-${index}`}>
          <div>
            <span className="cellTitle">{row.name}</span>
            <span className="cellSub">
              {row.kind}
              {row.detail ? ` · ${row.detail}` : ""}
            </span>
          </div>
          <span className={row.action === "validated" ? "badge badge-running" : "badge badge-good"}>
            <CheckCircle2 aria-hidden="true" />
            {provisioningActionLabel(row.action)}
          </span>
        </div>
      ))}
    </div>
  );
}

function summarizeResult(rows: ProvisioningAppliedResource[]): string {
  if (!rows.length) return "";
  const counts = rows.reduce<Record<string, number>>((acc, row) => {
    acc[row.action] = (acc[row.action] || 0) + 1;
    return acc;
  }, {});
  return Object.entries(counts)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([action, count]) =>
      translate("provisioning.actionCount", {
        count,
        action: provisioningActionLabel(action),
      }),
    )
    .join(" · ");
}

function provisioningActionLabel(action: string): string {
  if (action === "created") return translate("provisioning.action.created");
  if (action === "updated") return translate("provisioning.action.updated");
  if (action === "stored") return translate("provisioning.action.stored");
  if (action === "validated") return translate("provisioning.action.validated");
  return action;
}
