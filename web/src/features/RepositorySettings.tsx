import { useState } from "react";
import { DefinitionList, Field, Modal, Panel, ProbeNotice, SelectControl } from "../components/ui";
import { errorMessage, type GitSource, type ProbeResult } from "../lib/api";
import { useApp } from "../lib/app-context";
import { formatTime, shortSHA } from "../lib/format";
import { type GitAuthMethod, gitCredentialSecretValue } from "../lib/git-credential";
import { displayRepoURL } from "../lib/repo";
import {
  probePassed,
  reconnectCredentialPath,
  repositoryAccessLabel,
  repositoryLocationLocked,
} from "../lib/repository-settings";
import { useRouter } from "../lib/router";
import { translate } from "../shared/i18n";

type RepositoryAction = "rename" | "branch" | "location" | "credential" | null;

export function RepositorySettings({
  source,
  onChanged,
}: {
  source: GitSource;
  onChanged: () => void;
}) {
  const { api, notify } = useApp();
  const { navigate } = useRouter();
  const [action, setAction] = useState<RepositoryAction>(null);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [probing, setProbing] = useState(false);
  const [error, setError] = useState("");
  const locationLocked = repositoryLocationLocked(source);

  async function probeCurrentRepository() {
    setProbing(true);
    setProbe(null);
    setError("");
    try {
      setProbe(
        await api.probeGitSource({
          repo_url: source.repo_url,
          branch: source.branch || "main",
          subpath: source.subpath || undefined,
          creds_ref: source.creds_ref || undefined,
        }),
      );
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setProbing(false);
    }
  }

  async function removeSource() {
    const confirmed = window.confirm(translate("repository.removeConfirm", { name: source.name }));
    if (!confirmed) return;
    try {
      await api.deleteGitSource(source.id);
      notify("ok", translate("repository.removed", { name: source.name }));
      navigate("/");
    } catch (cause) {
      notify("error", errorMessage(cause));
    }
  }

  function finishChange() {
    setAction(null);
    setProbe(null);
    onChanged();
  }

  return (
    <>
      <Panel
        title={translate("repository.settings")}
        subtitle={translate("repository.settingsHint")}
        actions={
          <button
            className="button"
            type="button"
            disabled={probing}
            onClick={probeCurrentRepository}
          >
            {probing ? translate("repository.probing") : translate("registerApp.probe")}
          </button>
        }
      >
        <DefinitionList
          items={[
            [translate("registerApp.sourceName"), source.name],
            [translate("registerApp.repositoryURL"), displayRepoURL(source.repo_url)],
            [translate("release.branch"), source.branch || "main"],
            [translate("release.subpath"), source.subpath || translate("release.repoRoot")],
            [translate("repository.access"), repositoryAccessLabel(source)],
            [translate("trigger.detail.kind"), source.kind],
            [translate("release.registered"), formatTime(source.created_at)],
            [
              translate("appDetail.latestSynchronizedSource"),
              source.last_synced_commit
                ? shortSHA(source.last_synced_commit, 16)
                : translate("appDetail.notSynchronized"),
            ],
          ]}
        />

        {probe ? <ProbeNotice probe={probe} branch={source.branch || "main"} /> : null}
        {error ? <div className="inlineNotice error">{error}</div> : null}

        <section
          className="repositoryActions"
          aria-label={translate("repository.managementActions")}
        >
          <RepositoryActionRow
            label={translate("registerApp.sourceName")}
            value={source.name}
            action={translate("repository.rename")}
            onClick={() => setAction("rename")}
          />
          <RepositoryActionRow
            label={translate("repository.trackedBranch")}
            value={source.branch || "main"}
            action={translate("repository.changeBranch")}
            onClick={() => setAction("branch")}
          />
          <RepositoryActionRow
            label={translate("repository.location")}
            value={
              locationLocked
                ? translate("repository.locationLocked")
                : translate("repository.locationEditable")
            }
            action={locationLocked ? undefined : translate("repository.changeLocation")}
            onClick={locationLocked ? undefined : () => setAction("location")}
          />
          <RepositoryActionRow
            label={translate("repository.access")}
            value={repositoryAccessLabel(source)}
            action={translate("repository.reconnect")}
            onClick={() => setAction("credential")}
          />
        </section>

        <div className="dangerZone compact">
          <div>
            <strong>{translate("repository.removeSource")}</strong>
            <p>{translate("repository.removeSourceHint")}</p>
          </div>
          <button className="button danger" type="button" onClick={removeSource}>
            {translate("repository.removeSource")}
          </button>
        </div>
      </Panel>

      {action === "rename" ? (
        <RenameSourceDialog
          source={source}
          onClose={() => setAction(null)}
          onChanged={finishChange}
        />
      ) : null}
      {action === "branch" ? (
        <ChangeBranchDialog
          source={source}
          onClose={() => setAction(null)}
          onChanged={finishChange}
        />
      ) : null}
      {action === "location" && !locationLocked ? (
        <ChangeLocationDialog
          source={source}
          onClose={() => setAction(null)}
          onChanged={finishChange}
        />
      ) : null}
      {action === "credential" ? (
        <ReconnectCredentialDialog
          source={source}
          onClose={() => setAction(null)}
          onChanged={finishChange}
        />
      ) : null}
    </>
  );
}

function RepositoryActionRow({
  label,
  value,
  action,
  onClick,
}: {
  label: string;
  value: string;
  action?: string;
  onClick?: () => void;
}) {
  return (
    <div className="repositoryActionRow">
      <div>
        <strong>{label}</strong>
        <span>{value}</span>
      </div>
      {action && onClick ? (
        <button className="button" type="button" onClick={onClick}>
          {action}
        </button>
      ) : (
        <span className="status muted">{translate("repository.readOnly")}</span>
      )}
    </div>
  );
}

function RenameSourceDialog({ source, onClose, onChanged }: RepositoryDialogProps) {
  const { api, notify } = useApp();
  const [name, setName] = useState(source.name);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function save() {
    const nextName = name.trim();
    if (!nextName) {
      setError(translate("repository.sourceNameRequired"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      await api.patchGitSource(source.id, { name: nextName });
      notify("ok", translate("repository.renamed", { name: nextName }));
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={translate("repository.renameSource")}
      subtitle={translate("repository.renameSourceHint")}
      onClose={onClose}
    >
      <Field label={translate("registerApp.sourceName")}>
        <input value={name} onChange={(event) => setName(event.target.value)} />
      </Field>
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <DialogActions
        busy={busy}
        saveLabel={translate("repository.rename")}
        saveDisabled={!name.trim() || name.trim() === source.name}
        onClose={onClose}
        onSave={save}
      />
    </Modal>
  );
}

function ChangeBranchDialog({ source, onClose, onChanged }: RepositoryDialogProps) {
  const { api, notify } = useApp();
  const [branch, setBranch] = useState(source.branch || "main");
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [verifiedBranch, setVerifiedBranch] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const verified = verifiedBranch === branch.trim() && probePassed(probe);

  async function verify() {
    setBusy(true);
    setError("");
    setProbe(null);
    try {
      const result = await api.probeGitSource({
        repo_url: source.repo_url,
        branch: branch.trim(),
        subpath: source.subpath || undefined,
        creds_ref: source.creds_ref || undefined,
      });
      setProbe(result);
      setVerifiedBranch(branch.trim());
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    setBusy(true);
    setError("");
    try {
      await api.patchGitSource(source.id, { branch: branch.trim() });
      notify("ok", translate("repository.trackingBranch", { branch: branch.trim() }));
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={translate("repository.changeTrackedBranch")}
      subtitle={displayRepoURL(source.repo_url)}
      onClose={onClose}
    >
      <Field label={translate("release.branch")}>
        <input
          value={branch}
          onChange={(event) => {
            setBranch(event.target.value);
            setProbe(null);
            setVerifiedBranch("");
          }}
        />
      </Field>
      {probe ? <ProbeNotice probe={probe} branch={branch} /> : null}
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <DialogActions
        busy={busy}
        saveLabel={translate("repository.saveBranch")}
        saveDisabled={!verified || branch.trim() === (source.branch || "main")}
        onClose={onClose}
        onSave={save}
        secondaryLabel={translate("repository.probeBranch")}
        onSecondary={verify}
      />
    </Modal>
  );
}

function ChangeLocationDialog({ source, onClose, onChanged }: RepositoryDialogProps) {
  const { api, notify } = useApp();
  const [repoURL, setRepoURL] = useState(source.repo_url);
  const [subpath, setSubpath] = useState(source.subpath);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [verifiedLocation, setVerifiedLocation] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const location = `${repoURL.trim()}\n${subpath.trim()}`;
  const verified = verifiedLocation === location && probePassed(probe);

  function changeLocation(nextRepoURL: string, nextSubpath: string) {
    setRepoURL(nextRepoURL);
    setSubpath(nextSubpath);
    setProbe(null);
    setVerifiedLocation("");
  }

  async function verify() {
    setBusy(true);
    setError("");
    setProbe(null);
    try {
      const result = await api.probeGitSource({
        repo_url: repoURL.trim(),
        branch: source.branch || "main",
        subpath: subpath.trim() || undefined,
        creds_ref: source.creds_ref || undefined,
      });
      setProbe(result);
      setVerifiedLocation(location);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    setBusy(true);
    setError("");
    try {
      await api.patchGitSource(source.id, { repo_url: repoURL.trim(), subpath: subpath.trim() });
      notify("ok", translate("repository.locationChanged"));
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={translate("repository.changeLocation")}
      subtitle={translate("repository.changeLocationHint")}
      onClose={onClose}
    >
      <Field label={translate("registerApp.repositoryURL")}>
        <input value={repoURL} onChange={(event) => changeLocation(event.target.value, subpath)} />
      </Field>
      <Field label={translate("release.subpath")}>
        <input
          value={subpath}
          placeholder="(repo root)"
          onChange={(event) => changeLocation(repoURL, event.target.value)}
        />
      </Field>
      {probe ? <ProbeNotice probe={probe} branch={source.branch || "main"} /> : null}
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <DialogActions
        busy={busy}
        saveLabel={translate("repository.saveLocation")}
        saveDisabled={
          !verified || (repoURL.trim() === source.repo_url && subpath.trim() === source.subpath)
        }
        onClose={onClose}
        onSave={save}
        secondaryLabel={translate("registerApp.probe")}
        onSecondary={verify}
      />
    </Modal>
  );
}

function ReconnectCredentialDialog({ source, onClose, onChanged }: RepositoryDialogProps) {
  const { api, notify } = useApp();
  const [authMethod, setAuthMethod] = useState<GitAuthMethod>(source.creds_ref ? "pat" : "none");
  const [accessToken, setAccessToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [verified, setVerified] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function resetVerification() {
    setProbe(null);
    setVerified(false);
  }

  function credentialValue() {
    return gitCredentialSecretValue(authMethod, accessToken, username, password);
  }

  function probePayload() {
    const payload: Record<string, unknown> = {
      repo_url: source.repo_url,
      branch: source.branch || "main",
      subpath: source.subpath || undefined,
      auth_method: authMethod,
    };
    if (authMethod === "pat") payload.access_token = accessToken;
    if (authMethod === "basic") {
      payload.username = username;
      payload.password = password;
    }
    return payload;
  }

  async function verify() {
    if (authMethod !== "none" && !credentialValue()) {
      setError(
        authMethod === "pat"
          ? translate("repository.accessTokenRequired")
          : translate("repository.usernamePasswordRequired"),
      );
      return;
    }
    setBusy(true);
    setError("");
    setProbe(null);
    try {
      const result = await api.probeGitSource(probePayload());
      setProbe(result);
      setVerified(probePassed(result));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    setBusy(true);
    setError("");
    try {
      if (authMethod === "none") {
        await api.patchGitSource(source.id, { creds_ref: "" });
        notify("ok", translate("repository.changedToPublic"));
      } else {
        const path = reconnectCredentialPath(source);
        await api.setVariable({
          path,
          value: credentialValue(),
          is_secret: true,
          description: `Git credential for source ${source.name}`,
        });
        await api.patchGitSource(source.id, { creds_ref: path });
        notify("ok", translate("repository.credentialReplaced"));
      }
      onChanged();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={translate("repository.reconnectAccess")}
      subtitle={displayRepoURL(source.repo_url)}
      onClose={onClose}
    >
      <Field label={translate("settings.authentication")}>
        <SelectControl
          value={authMethod}
          onChange={(value) => {
            setAuthMethod(value);
            resetVerification();
          }}
          ariaLabel={translate("repository.authentication")}
          options={[
            { value: "pat", label: translate("repository.personalAccessToken") },
            { value: "basic", label: translate("repository.usernamePassword") },
            { value: "none", label: translate("repository.public") },
          ]}
        />
      </Field>
      {authMethod === "pat" ? (
        <Field label={translate("registerApp.accessToken")}>
          <input
            type="password"
            autoComplete="new-password"
            value={accessToken}
            onChange={(event) => {
              setAccessToken(event.target.value);
              resetVerification();
            }}
          />
        </Field>
      ) : null}
      {authMethod === "basic" ? (
        <div className="formGrid two">
          <Field label={translate("registerApp.username")}>
            <input
              autoComplete="username"
              value={username}
              onChange={(event) => {
                setUsername(event.target.value);
                resetVerification();
              }}
            />
          </Field>
          <Field label={translate("repository.passwordOrToken")}>
            <input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(event) => {
                setPassword(event.target.value);
                resetVerification();
              }}
            />
          </Field>
        </div>
      ) : null}
      {probe ? <ProbeNotice probe={probe} branch={source.branch || "main"} /> : null}
      {error ? <div className="inlineNotice error">{error}</div> : null}
      <DialogActions
        busy={busy}
        saveLabel={translate("repository.saveAccess")}
        saveDisabled={!verified}
        onClose={onClose}
        onSave={save}
        secondaryLabel={translate("repository.probeAccess")}
        onSecondary={verify}
      />
    </Modal>
  );
}

type RepositoryDialogProps = {
  source: GitSource;
  onClose: () => void;
  onChanged: () => void;
};

function DialogActions({
  busy,
  saveLabel,
  saveDisabled,
  onClose,
  onSave,
  secondaryLabel,
  onSecondary,
}: {
  busy: boolean;
  saveLabel: string;
  saveDisabled: boolean;
  onClose: () => void;
  onSave: () => void;
  secondaryLabel?: string;
  onSecondary?: () => void;
}) {
  return (
    <footer className="dialogFooter">
      <span>
        {secondaryLabel && onSecondary ? (
          <button className="button" type="button" disabled={busy} onClick={onSecondary}>
            {busy ? translate("repository.checking") : secondaryLabel}
          </button>
        ) : null}
      </span>
      <div className="dialogFooterActions">
        <button className="button" type="button" disabled={busy} onClick={onClose}>
          {translate("common.cancel")}
        </button>
        <button
          className="button primary"
          type="button"
          disabled={busy || saveDisabled}
          onClick={onSave}
        >
          {busy ? translate("common.saving") : saveLabel}
        </button>
      </div>
    </footer>
  );
}
