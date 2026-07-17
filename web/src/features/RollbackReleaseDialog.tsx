import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { DefinitionList, Field, Modal } from "../components/ui";
import { errorMessage, type HistoryItem, type ReleaseRollbackResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatTime, shortSHA } from "../lib/format";
import { Link } from "../lib/router";

export function RollbackReleaseDialog({
  appKey,
  target,
  active,
  onClose,
  onRolledBack,
}: {
  appKey: string;
  target: HistoryItem;
  active: HistoryItem | null;
  onClose: () => void;
  onRolledBack: (result: ReleaseRollbackResult) => void;
}) {
  const { api, settings, notify } = useApp();
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function rollback() {
    const normalizedReason = reason.trim();
    if (!normalizedReason) return;
    setBusy(true);
    setError("");
    try {
      const result = await api.rollbackAppRelease(appKey, target.id, normalizedReason);
      notify("ok", `Activated ${shortSHA(result.commit, 12)} for ${result.app}.`);
      onRolledBack(result);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      id="rollbackReleaseDialog"
      title={`Rollback Release — ${appKey}`}
      subtitle="Activate a worker-ready historical release for new runs."
      onClose={onClose}
    >
      <DefinitionList
        items={[
          ["Current release", active ? <><code>{shortSHA(active.id, 12)}</code> · commit <code>{shortSHA(active.commit_sha, 12)}</code></> : "unknown"],
          ["Target release", <><code>{shortSHA(target.id, 12)}</code> · commit <code>{shortSHA(target.commit_sha, 12)}</code></>],
          ["Target release ID", <code>{target.id}</code>],
          ["Originally published", `${target.created_by || "system"} · ${formatTime(target.created_at)}`],
          ["Actor", settings.actor || "(not set)"],
        ]}
      />
      <div className="inlineNotice">
        This moves the active release pointer to the stored execution bundle. It does not synchronize Git, install dependencies,
        or rebuild source. Existing runs keep their pinned release; new runs use this target.
      </div>
      {!settings.actor ? (
        <div className="inlineNotice error">
          Rollback requires an audit actor. Set one in <Link to="/settings">Settings</Link>.
        </div>
      ) : null}
      <Field label="Rollback reason" hint="Required and recorded in the app audit trail.">
        <textarea
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder="Why is this historical release being restored?"
          rows={3}
          autoFocus
        />
      </Field>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button className="button danger" type="button" onClick={rollback} disabled={busy || !settings.actor || !reason.trim()}>
            <RotateCcw size={16} aria-hidden="true" />
            {busy ? "Rolling back…" : "Rollback release"}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
