import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { DefinitionList, Field, Modal } from "../components/ui";
import { errorMessage, type HistoryItem, type ReleaseRollbackResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatTime, shortSHA } from "../lib/format";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

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
      notify(
        "ok",
        translate("release.activated", { commit: shortSHA(result.commit, 12), app: result.app }),
      );
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
      title={translate("release.rollbackNamed", { app: appKey })}
      subtitle={translate("release.rollbackHint")}
      onClose={onClose}
    >
      <DefinitionList
        items={[
          [
            translate("release.current"),
            active ? (
              <>
                <code>{shortSHA(active.id, 12)}</code> · commit{" "}
                <code>{shortSHA(active.commit_sha, 12)}</code>
              </>
            ) : (
              translate("release.unknown")
            ),
          ],
          [
            translate("release.target"),
            <>
              <code>{shortSHA(target.id, 12)}</code> · commit{" "}
              <code>{shortSHA(target.commit_sha, 12)}</code>
            </>,
          ],
          [translate("release.targetID"), <code>{target.id}</code>],
          [
            translate("release.originallyPublished"),
            `${target.created_by || "system"} · ${formatTime(target.created_at)}`,
          ],
          [translate("settings.actor"), settings.actor || translate("info.notSet")],
        ]}
      />
      <div className="inlineNotice">{translate("release.rollbackNotice")}</div>
      {!settings.actor ? (
        <div className="inlineNotice error">
          {translate("release.rollbackActorRequired")}{" "}
          <Link to="/settings">{translate("navigation.settings")}</Link>.
        </div>
      ) : null}
      <Field
        label={translate("release.rollbackReason")}
        hint={translate("release.rollbackReasonHint")}
      >
        <textarea
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          placeholder={translate("release.rollbackReasonPlaceholder")}
          rows={3}
        />
      </Field>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={busy}>
            {translate("common.cancel")}
          </button>
          <button
            className="button danger"
            type="button"
            onClick={rollback}
            disabled={busy || !settings.actor || !reason.trim()}
          >
            <RotateCcw size={16} aria-hidden="true" />
            {busy ? translate("release.rollingBack") : translate("release.rollback")}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
