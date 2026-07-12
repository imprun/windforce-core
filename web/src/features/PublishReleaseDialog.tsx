import { useState } from "react";
import type { GitSource, SyncResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { shortSHA } from "../lib/format";
import { DefinitionList, Field, Modal } from "../components/ui";
import { Link } from "../lib/router";

export function PublishReleaseDialog({
  source,
  onClose,
  onPublished,
}: {
  source: GitSource;
  onClose: () => void;
  onPublished: (result: SyncResult) => void;
}) {
  const { api, settings, notify } = useApp();
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function handlePublish() {
    setBusy(true);
    setError("");
    try {
      const result = await api.deployGitSource(source.id, message.trim());
      notify("ok", `Published ${result.app} at ${shortSHA(result.commit, 12)}.`);
      onPublished(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      id="publishReleaseDialog"
      title={`Publish Release — ${source.name}`}
      subtitle="Validates the repository source at HEAD and publishes the worker-visible contract."
      onClose={onClose}
    >
      <DefinitionList
        items={[
          ["Repository", source.repo_url],
          ["Branch", source.branch || "main"],
          ["Subpath", source.subpath || "(repo root)"],
          ["Current release", source.last_synced_commit ? shortSHA(source.last_synced_commit, 12) : "not released yet"],
          ["Actor", settings.actor || "(not set)"],
        ]}
      />
      {!settings.actor ? (
        <div className="inlineNotice error">
          Publishing requires an audit actor. Set one in <Link to="/settings">Settings</Link>.
        </div>
      ) : null}
      <Field label="Release note" hint="Recorded in release history (optional).">
        <input
          id="publishReleaseMessage"
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="What changed in this release?"
        />
      </Field>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            className="button primary"
            type="button"
            onClick={handlePublish}
            disabled={busy || !settings.actor}
          >
            {busy ? "Publishing…" : "Publish Release"}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
