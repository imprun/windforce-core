import { useState } from "react";
import type { GitSource, ProbeResult, RegisterSourcePayload } from "../lib/api";
import { useApp } from "../lib/app-context";
import { Field, Modal } from "../components/ui";

type AuthMethod = "none" | "token" | "basic";

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
  const [authMethod, setAuthMethod] = useState<AuthMethod>("none");
  const [accessToken, setAccessToken] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [credsRef, setCredsRef] = useState("");
  const [busy, setBusy] = useState(false);
  const [probe, setProbe] = useState<ProbeResult | null>(null);
  const [error, setError] = useState("");

  function buildPayload(): RegisterSourcePayload {
    const payload: RegisterSourcePayload = { name: name.trim(), repo_url: repoURL.trim() };
    if (branch.trim()) payload.branch = branch.trim();
    if (subpath.trim()) payload.subpath = subpath.trim();
    if (credsRef.trim()) payload.creds_ref = credsRef.trim();
    if (authMethod === "token") {
      payload.auth_method = "token";
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
      const payload = buildPayload();
      const result = await api.probeGitSource({ ...payload, name: undefined });
      setProbe(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleRegister() {
    if (!name.trim() || !repoURL.trim()) {
      setError("App name and repository URL are required.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      const created = await api.registerGitSource(buildPayload());
      notify("ok", `Registered ${created.name}.`);
      onRegistered(created);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function handleSample() {
    setBusy(true);
    setError("");
    try {
      const result = await api.createSample("echo");
      notify("ok", `Created sample app ${result.sync_result.app}.`);
      onRegistered(result.source);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      id="registerAppDialog"
      title="Register App"
      subtitle="Point at the repository source that builds this app. Registration validates access, branch, and windforce.json before saving."
      onClose={onClose}
      wide
    >
      <div className="formGrid">
        <Field label="App name">
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="echo" autoFocus />
        </Field>
        <Field label="Repository URL">
          <input
            value={repoURL}
            onChange={(event) => setRepoURL(event.target.value)}
            placeholder="https://github.com/org/repo.git"
          />
        </Field>
        <Field label="Branch">
          <input value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="main" />
        </Field>
        <Field label="Subpath" hint="Repository directory used as the app root. Leave empty for the repo root.">
          <input value={subpath} onChange={(event) => setSubpath(event.target.value)} placeholder="apps/echo" />
        </Field>
        <Field label="Git auth">
          <select value={authMethod} onChange={(event) => setAuthMethod(event.target.value as AuthMethod)}>
            <option value="none">Public (no credential)</option>
            <option value="token">Access token</option>
            <option value="basic">Username + password</option>
          </select>
        </Field>
        {authMethod === "token" ? (
          <Field label="Access token" hint="Stored as a workspace secret variable; creds ref below is optional.">
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
            <Field label="Username">
              <input value={username} onChange={(event) => setUsername(event.target.value)} autoComplete="off" />
            </Field>
            <Field label="Password">
              <input
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="off"
              />
            </Field>
          </>
        ) : null}
        <Field label="Creds ref" hint="Existing workspace variable path holding the git credential (optional).">
          <input value={credsRef} onChange={(event) => setCredsRef(event.target.value)} placeholder="secrets/git/token" />
        </Field>
      </div>

      {probe ? (
        <div className={probe.reachable ? "inlineNotice ok" : "inlineNotice error"}>
          {probe.reachable
            ? `Repository reachable. Branch ${probe.branch || branch} ${probe.branch_exists ? "exists" : "was not found"}.` +
              (probe.branches?.length ? ` Remote branches: ${probe.branches.slice(0, 8).join(", ")}.` : "")
            : probe.error || "Repository is not reachable."}
        </div>
      ) : null}
      {error ? <div className="inlineNotice error">{error}</div> : null}

      <footer className="dialogFooter">
        <button className="button" type="button" disabled={busy} onClick={handleSample}>
          Create sample app
        </button>
        <div className="dialogFooterActions">
          <button className="button" type="button" disabled={busy || !repoURL.trim()} onClick={handleProbe}>
            Probe repository
          </button>
          <button className="button primary" type="button" disabled={busy} onClick={handleRegister}>
            Register App
          </button>
        </div>
      </footer>
    </Modal>
  );
}
