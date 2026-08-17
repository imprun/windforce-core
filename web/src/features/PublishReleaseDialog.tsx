import { useState } from "react";
import { DefinitionList, Field, Modal } from "../components/ui";
import { type DeployResult, errorMessage, type GitSource } from "../lib/api";
import { useApp } from "../lib/app-context";
import { appSettingsPath } from "../lib/app-settings-navigation";
import { shortSHA } from "../lib/format";
import { displayRepoURL } from "../lib/repo";
import { Link } from "../lib/router";
import { translate } from "../shared/i18n";

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
      notify(
        "ok",
        translate("release.published", { app: result.app, commit: shortSHA(result.commit, 12) }),
      );
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
      title={translate("release.publishNamed", { app: appKey || source.name })}
      subtitle={translate("release.publishDialogHint")}
      onClose={onClose}
    >
      <DefinitionList
        items={[
          [translate("apps.column.repositorySource"), source.name],
          [translate("audit.repository"), displayRepoURL(source.repo_url)],
          [translate("release.branch"), source.branch || "main"],
          [translate("release.subpath"), source.subpath || translate("release.repoRoot")],
          [
            translate("release.active"),
            activeCommit ? (
              <code>{shortSHA(activeCommit, 12)}</code>
            ) : (
              translate("release.notPublished")
            ),
          ],
          [
            translate("release.latestSynchronized"),
            latestSyncedCommit ? (
              <code>{shortSHA(latestSyncedCommit, 12)}</code>
            ) : (
              translate("release.notSynchronized")
            ),
          ],
          [translate("settings.actor"), settings.actor || translate("info.notSet")],
        ]}
      />
      {latestSyncedCommit ? (
        <div className="inlineNotice">
          {translate("release.publishPreparationPrefix")}{" "}
          <code>{shortSHA(latestSyncedCommit, 12)}</code>.{" "}
          {translate("release.publishPreparationSuffix")}
        </div>
      ) : (
        <div className="inlineNotice error">
          {translate("release.noSynchronizedSource")}{" "}
          <Link to={appSettingsPath(source.id, "repository")}>
            {translate("release.syncRepositorySource")}
          </Link>{" "}
          {translate("release.beforePublishing")}
        </div>
      )}
      {!settings.actor ? (
        <div className="inlineNotice error">
          {translate("release.actorRequired")}{" "}
          <Link to="/settings">{translate("navigation.settings")}</Link>.
        </div>
      ) : null}
      <Field label={translate("release.note")} hint={translate("release.noteHint")}>
        <input
          id="publishReleaseMessage"
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder={translate("release.notePlaceholder")}
        />
      </Field>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <footer className="dialogFooter">
        <span />
        <div className="dialogFooterActions">
          <button className="button" type="button" onClick={onClose} disabled={publishing}>
            {translate("common.cancel")}
          </button>
          <button
            className="button primary"
            type="button"
            onClick={handlePublish}
            disabled={publishing || !settings.actor || !latestSyncedCommit}
          >
            {publishing ? translate("release.publishing") : translate("release.publishLatest")}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
