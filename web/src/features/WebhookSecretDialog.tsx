import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { Modal } from "../components/ui";
import { translate } from "../shared/i18n";

export function WebhookSecretDialog({
  secret,
  endpoint,
  onClose,
}: {
  secret: string;
  endpoint?: string;
  onClose: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [completionCopied, setCompletionCopied] = useState(false);
  const notificationCenterSourceKey = sourceKeyFromNotificationCenterEndpoint(endpoint || "");
  const notificationCenterCompletion = notificationCenterSourceKey
    ? JSON.stringify({ signingSecret: secret, enabled: true }, null, 2)
    : "";

  async function copySecret() {
    await navigator.clipboard.writeText(secret);
    setCopied(true);
  }

  async function copyNotificationCenterCompletion() {
    await navigator.clipboard.writeText(notificationCenterCompletion);
    setCompletionCopied(true);
  }

  return (
    <Modal
      title={translate("webhook.saveSigningSecret")}
      subtitle={translate("webhook.secretShownOnce")}
      onClose={onClose}
      id="webhookSecretDialog"
    >
      <div className="secretReveal">
        <code id="webhookSigningSecret">{secret}</code>
        <button className="button" type="button" onClick={copySecret}>
          {copied ? <Check size={16} aria-hidden="true" /> : <Copy size={16} aria-hidden="true" />}
          {copied ? translate("common.copied") : translate("webhook.copySecret")}
        </button>
      </div>
      <div className="inlineNotice warning">
        {translate("webhook.configureReceiver")} <span className="mono">X-Windforce-Signature</span>{" "}
        header.
      </div>
      {notificationCenterSourceKey ? (
        <div className="notificationCenterHandoff">
          <div>
            <strong>{translate("webhook.notificationCenterSource")}</strong>
            <p>
              {translate("webhook.openSourcePrefix")}{" "}
              <span className="mono">{notificationCenterSourceKey}</span>{" "}
              {translate("webhook.openSourceSuffix")}
            </p>
            <code>POST /api/v1/admin/sources/{notificationCenterSourceKey}/signing-secret</code>
          </div>
          <pre>{notificationCenterCompletion}</pre>
          <button className="button" type="button" onClick={copyNotificationCenterCompletion}>
            {completionCopied ? (
              <Check size={16} aria-hidden="true" />
            ) : (
              <Copy size={16} aria-hidden="true" />
            )}
            {completionCopied
              ? translate("common.copied")
              : translate("webhook.copyNotificationCenterBody")}
          </button>
        </div>
      ) : null}
      <footer className="dialogFooter webhookSecretFooter">
        <span className="fieldHint">{translate("webhook.closeDoesNotDisable")}</span>
        <button className="button primary" type="button" onClick={onClose}>
          {translate("webhook.savedSecret")}
        </button>
      </footer>
    </Modal>
  );
}

function sourceKeyFromNotificationCenterEndpoint(endpoint: string): string {
  try {
    const url = new URL(endpoint);
    const match = url.pathname.match(/\/api\/v1\/ingress\/([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)$/);
    return match?.[1] || "";
  } catch {
    return "";
  }
}
