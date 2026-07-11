"use client";

import type { DeploymentRequest } from "@/entities/app";
import type { GitSource } from "@/entities/git-source";
import { formatDate, shortID } from "@/shared/lib/format";

type RequestDialogProps = {
  source: GitSource | null;
  actor: string;
  busy: boolean;
  error: string;
  onClose: () => void;
  onRequest: (message: string) => Promise<void>;
  onOpenSettings?: () => void;
};

type ReviewDialogProps = {
  request: DeploymentRequest | null;
  source: GitSource | null;
  actor: string;
  busy: boolean;
  error: string;
  onClose: () => void;
  onDeploy: (message: string) => Promise<void>;
  onReject: (message: string) => Promise<void>;
  onOpenSettings?: () => void;
};

export function RequestDeploymentDialog({ source, actor, busy, error, onClose, onRequest, onOpenSettings }: RequestDialogProps) {
  if (!source) return null;

  async function submit(form: HTMLFormElement) {
    const formData = new FormData(form);
    const message = String(formData.get("message") || "").trim();
    if (!actor.trim()) return;
    await onRequest(message);
  }

  return (
    <div id="requestDeploymentDialog" className="modalBackdrop" role="presentation">
      <form
        className="modal"
        aria-label="Request deployment"
        onSubmit={(event) => {
          event.preventDefault();
          void submit(event.currentTarget);
        }}
      >
        <header className="dialogHeader">
          <div>
            <h2>Request Deployment</h2>
            <p>{source.repo_url}</p>
          </div>
          <button className="button" type="button" onClick={onClose}>
            Cancel
          </button>
        </header>
        <div className="detailGrid two">
          <Field label="FCode" value={source.name} />
          <Field label="Branch" value={source.branch || "main"} />
          <Field label="Subpath" value={source.subpath || "root"} />
          <Field label="Current release" value={`${formatDate(source.last_synced_at)} / ${shortID(source.last_synced_commit, 14)}`} />
        </div>
        <label className="field">
          Request note
          <textarea id="requestDeploymentMessage" name="message" placeholder="validation context, change reason, rollout note" />
        </label>
        <p className={actor.trim() ? "hint" : "hint warn"}>
          {actor.trim() ? `Requester: ${actor}` : "Set Actor in Settings before creating a deployment request."}
        </p>
        {error ? <p className="hint dangerText">{error}</p> : null}
        <div className="actions end">
          {!actor.trim() && onOpenSettings ? (
            <button
              className="button"
              type="button"
              onClick={() => {
                onClose();
                onOpenSettings();
              }}
            >
              Set Actor
            </button>
          ) : null}
          <button className="button primary" type="submit" disabled={busy || !actor.trim()}>
            {busy ? "Validating..." : "Create Request"}
          </button>
        </div>
      </form>
    </div>
  );
}

export function ReviewDeploymentRequestDialog({ request, source, actor, busy, error, onClose, onDeploy, onReject, onOpenSettings }: ReviewDialogProps) {
  if (!request) return null;
  const activeRequest = request;

  async function submit(form: HTMLFormElement, action: "deploy" | "reject") {
    const formData = new FormData(form);
    const confirmName = String(formData.get("confirmName") || "").trim();
    const message = String(formData.get("message") || "").trim();
    if (confirmName !== activeRequest.source_name || !actor.trim()) return;
    if (action === "deploy") {
      await onDeploy(message);
      return;
    }
    await onReject(message);
  }

  return (
    <div id="reviewDeploymentRequestDialog" className="modalBackdrop" role="presentation">
      <form
        className="modal"
        aria-label="Review deployment request"
        onSubmit={(event) => {
          event.preventDefault();
          void submit(event.currentTarget, "deploy");
        }}
      >
        <header className="dialogHeader">
          <div>
            <h2>Review Deployment Request</h2>
            <p>{activeRequest.source_name} / {activeRequest.app_key || "pending app"}</p>
          </div>
          <button className="button" type="button" onClick={onClose}>
            Cancel
          </button>
        </header>
        <div className="detailGrid two">
          <Field label="Requester" value={activeRequest.requested_by} />
          <Field label="Requested" value={formatDate(activeRequest.created_at)} />
          <Field label="Target commit" value={shortID(activeRequest.target_commit, 14)} />
          <Field label="Current commit" value={shortID(activeRequest.current_commit, 14)} />
          <Field label="Branch" value={activeRequest.branch || source?.branch || "main"} />
          <Field label="Subpath" value={activeRequest.subpath || source?.subpath || "root"} />
        </div>
        <div className="releaseBlock">
          <span className="eyebrow">Request note</span>
          <strong>{activeRequest.request_message || "No note"}</strong>
          <p>{activeRequest.actions_count} actions / {activeRequest.entrypoint || "entrypoint not set"}</p>
        </div>
        <label className="field">
          Type FCode name
          <input id="reviewDeploymentConfirmInput" name="confirmName" placeholder={activeRequest.source_name} autoComplete="off" />
        </label>
        <label className="field">
          Operator note
          <textarea id="reviewDeploymentMessage" name="message" placeholder="approval, rejection, rollback, or rollout context" />
        </label>
        <p className={actor.trim() ? "hint" : "hint warn"}>
          {actor.trim() ? `Operator: ${actor}` : "Set Actor in Settings before reviewing deployment requests."}
        </p>
        {error ? <p className="hint dangerText">{error}</p> : null}
        <div className="actions end">
          {!actor.trim() && onOpenSettings ? (
            <button
              className="button"
              type="button"
              onClick={() => {
                onClose();
                onOpenSettings();
              }}
            >
              Set Actor
            </button>
          ) : null}
          <button
            className="button dangerGhost"
            type="button"
            disabled={busy || !actor.trim()}
            onClick={(event) => {
              const form = event.currentTarget.form;
              if (form) void submit(form, "reject");
            }}
          >
            Reject
          </button>
          <button className="button primary" type="submit" disabled={busy || !actor.trim()}>
            {busy ? "Deploying..." : "Approve & Deploy"}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="kv">
      <span>{label}</span>
      <strong>{value || "-"}</strong>
    </div>
  );
}
