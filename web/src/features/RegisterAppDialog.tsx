import { useState } from "react";
import { Field, Modal, ProbeNotice, SelectControl } from "../components/ui";
import type { GitSource, ProbeResult, RegisterSourcePayload } from "../lib/api";
import { useApp } from "../lib/app-context";
import {
  defaultGitCredentialPath,
  type GitAuthMethod,
  gitCredentialSecretValue,
} from "../lib/git-credential";
import { sourceErrorMessage } from "../lib/repository-settings";
import { translate } from "../shared/i18n";

export function RegisterAppDialog({
  onClose,
  onRegistered,
}: {
  onClose: () => void;
  onRegistered: (source: GitSource) => void;
}) {
  const { api, notify } = useApp();
  const [name, setName] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [branch, setBranch] = useState("main");
  const [subpath, setSubpath] = useState("");
  const [authMethod, setAuthMethod] = useState<GitAuthMethod>("none");
  const [accessToken, setAccessToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [credsRef, setCredsRef] = useState("");
  const [busy, setBusy] = useState(false);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [error, setError] = useState("");

  function invalidateProbe() {
    setProbe(null);
  }

  function buildPayload(): RegisterSourcePayload {
    const payload: RegisterSourcePayload = { name: name.trim(), repo_url: repoURL.trim() };
    if (branch.trim()) payload.branch = branch.trim();
    if (subpath.trim()) payload.subpath = subpath.trim();
    if (credsRef.trim()) payload.creds_ref = credsRef.trim();
    return payload;
  }

  function buildProbePayload(): Record<string, unknown> {
    const payload: Record<string, unknown> = buildPayload();
    delete payload.name;
    if (authMethod === "pat") {
      payload.auth_method = "pat";
      payload.access_token = accessToken;
    } else if (authMethod === "basic") {
      payload.auth_method = "basic";
      payload.username = username;
      payload.password = password;
    }
    return payload;
  }

  async function handleProbe() {
    setBusy(true);
    setError("");
    setProbe(null);
    try {
      const result = await api.probeGitSource(buildProbePayload());
      setProbe(result);
    } catch (cause) {
      setError(sourceErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleRegister() {
    if (!name.trim() || !repoURL.trim()) {
      setError(translate("registerApp.nameRepositoryRequired"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const credentialValue = gitCredentialSecretValue(authMethod, accessToken, username, password);
      const credentialPath =
        credsRef.trim() || (credentialValue ? defaultGitCredentialPath(name) : "");
      if (authMethod === "pat" && !credentialValue && !credentialPath) {
        setError(translate("registerApp.accessTokenRequired"));
        return;
      }
      if (authMethod === "basic" && !credentialValue && !credentialPath) {
        setError(translate("registerApp.basicRequired"));
        return;
      }
      if (credentialValue) {
        await api.setVariable({
          path: credentialPath,
          value: credentialValue,
          is_secret: true,
          description: `Git credential for source ${name.trim()}`,
        });
      }
      const payload = buildPayload();
      if (credentialPath) payload.creds_ref = credentialPath;
      const created = await api.registerGitSource(payload);
      notify("ok", translate("registerApp.registered", { name: created.name }));
      onRegistered(created);
    } catch (cause) {
      setError(sourceErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleSample() {
    setBusy(true);
    setError("");
    try {
      const result = await api.createSample("echo");
      notify("ok", translate("registerApp.sampleCreated", { app: result.sync_result.app }));
      onRegistered(result.source);
    } catch (cause) {
      setError(sourceErrorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      id="registerAppDialog"
      title={translate("apps.register")}
      subtitle={translate("registerApp.subtitle")}
      onClose={onClose}
      wide
    >
      <div className="formGrid">
        <Field
          label={translate("registerApp.sourceName")}
          hint={translate("registerApp.sourceNameHint")}
        >
          <input
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="echo"
          />
        </Field>
        <Field label={translate("registerApp.repositoryURL")}>
          <input
            value={repoURL}
            onChange={(event) => {
              setRepoURL(event.target.value);
              invalidateProbe();
            }}
            placeholder="https://github.com/org/repo.git"
          />
        </Field>
        <Field label={translate("release.branch")}>
          <input
            value={branch}
            onChange={(event) => {
              setBranch(event.target.value);
              invalidateProbe();
            }}
            placeholder="main"
          />
        </Field>
        <Field label={translate("release.subpath")} hint={translate("registerApp.subpathHint")}>
          <input
            value={subpath}
            onChange={(event) => {
              setSubpath(event.target.value);
              invalidateProbe();
            }}
            placeholder="apps/echo"
          />
        </Field>
        <Field label={translate("registerApp.gitAuth")}>
          <SelectControl
            value={authMethod}
            onChange={setAuthMethod}
            ariaLabel={translate("registerApp.gitAuth")}
            options={[
              { value: "none", label: translate("registerApp.authPublic") },
              { value: "pat", label: translate("registerApp.accessToken") },
              { value: "basic", label: translate("registerApp.usernamePassword") },
            ]}
          />
        </Field>
        {authMethod === "pat" ? (
          <Field
            label={translate("registerApp.accessToken")}
            hint={translate("registerApp.accessTokenHint")}
          >
            <input
              type="password"
              value={accessToken}
              onChange={(event) => setAccessToken(event.target.value)}
              autoComplete="off"
            />
          </Field>
        ) : null}
        {authMethod === "basic" ? (
          <>
            <Field label={translate("registerApp.username")}>
              <input
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                autoComplete="off"
              />
            </Field>
            <Field label={translate("registerApp.password")}>
              <input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="off"
              />
            </Field>
          </>
        ) : null}
        <Field
          label={translate("registerApp.credentialPath")}
          hint={translate("registerApp.credentialPathHint")}
        >
          <input
            value={credsRef}
            onChange={(event) => setCredsRef(event.target.value)}
            placeholder={name.trim() ? defaultGitCredentialPath(name) : "git/source/credential"}
          />
        </Field>
      </div>

      {probe ? <ProbeNotice probe={probe} branch={branch} /> : null}
      {error ? <div className="inlineNotice error">{error}</div> : null}

      <footer className="dialogFooter">
        <button className="button" type="button" disabled={busy} onClick={handleSample}>
          {translate("registerApp.createSample")}
        </button>
        <div className="dialogFooterActions">
          <button
            className="button"
            type="button"
            data-ui-guide="probe-app-repository"
            disabled={busy || !repoURL.trim()}
            onClick={handleProbe}
          >
            {translate("registerApp.probe")}
          </button>
          <button className="button primary" type="button" disabled={busy} onClick={handleRegister}>
            {translate("apps.register")}
          </button>
        </div>
      </footer>
    </Modal>
  );
}
