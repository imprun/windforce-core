import { useState } from "react";
import { DefinitionList, Field, Modal } from "../components/ui";
import { type DeployResult, errorMessage, type GitSource } from "../lib/api";
import { useApp } from "../lib/app-context";
import { shortSHA } from "../lib/format";
import { Link } from "../lib/router";

export function PublishReleaseDialog({
  source,
  appKey,
  activeCommit,
  onClose,
  onPublished,
}: {
  source: GitSource;
  appKey?: string;
  activeCommit?: string;
  onClose: () => void;
  onPublished: (result: DeployResult) => void;
}) {
  const { api, settings, notify } = useApp();
  const [message, setMessage] = useState("");
  const [publishing, setPublishing] = useState(false);
  const [error, setError] = useState("");

  async function handlePublish() {
    if (!source.last_synced_commit) return;
    setPublishing(true);
    setError("");
    try {
      const result = await api.deployGitSource(source.id, message.trim());
      notify("ok", `Published ${result.app} at ${shortSHA(result.commit, 12)}.`);
      onPublished(result);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setPublishing(false);
    }
  }

  const latestSyncedCommit = source.last_synced_commit || "";

  return (
    <Modal
      id="publishReleaseDialog"
      title={`Publish Release — ${appKey || source.name}`}
      subtitle="Prepare and publish the latest synchronized source revision to workers."
      onClose={onClose}
    >
      <DefinitionList
        items={[
          ["Repository source", source.name],
          ["Repository", source.repo_url],
          ["Branch", source.branch || "main"],
          ["Subpath", source.subpath || "(repo root)"],
          [
            "Active release",
            activeCommit ? <code>{shortSHA(activeCommit, 12)}</code> : "not published yet",
          ],
          [
            "Latest synchronized",
            latestSyncedCommit ? (
              <code>{shortSHA(latestSyncedCommit, 12)}</code>
            ) : (
              "not synchronized yet"
            ),
          ],
          ["Actor", settings.actor || "(not set)"],
        ]}
      />
      {latestSyncedCommit ? (
        <div className="inlineNotice">
          Publishing installs locked dependencies, validates the entrypoint, stores a worker-ready
          execution bundle, and activates commit <code>{shortSHA(latestSyncedCommit, 12)}</code>.
          The active release stays unchanged if preparation fails.
        </div>
      ) : (
        <div className="inlineNotice error">
          No synchronized source is available.{" "}
          <Link to={`/apps/${source.id}/repository`}>Sync the repository source</Link> before
          publishing.
        </div>
      )}
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
          <button className="button" type="button" onClick={onClose} disabled={publishing}>
            Cancel
          </button>
          <button
            className="button primary"
            type="button"
            onClick={handlePublish}
            disabled={publishing || !settings.actor || !latestSyncedCommit}
          >
            {publishing ? "Publishing…" : "Publish latest synchronized"}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
