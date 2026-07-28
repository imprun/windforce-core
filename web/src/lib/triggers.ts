import type { SystemInfo, TriggerDefinition, TriggerKind, TriggerPayload } from "./api";

export type TriggerDraft = {
  name: string;
  kind: TriggerKind;
  action: string;
  credentialRef: string;
  webhookSecret: string;
  signatureHeader: string;
  deliveryIDHeader: string;
  correlationHeader: string;
  inputMode: "json" | "raw";
  cron: string;
  timezone: string;
  scheduleInput: string;
  rabbitMQURL: string;
  queue: string;
  prefetch: string;
  concurrency: string;
  consumerTag: string;
  completionMode: "none" | "poll" | "callback" | "publish";
  callbackEndpoint: string;
  callbackSigningSecret: string;
  publishRabbitMQURL: string;
  publishExchange: string;
  publishRoutingKey: string;
  responseMode: "async" | "wait";
  responseTimeout: string;
};

export type TriggerPayloadResult =
  | { payload: TriggerPayload; error?: never }
  | { payload?: never; error: string };

function configString(config: Record<string, unknown>, key: string, fallback = ""): string {
  const value = config[key];
  return typeof value === "string" ? value : fallback;
}

function configNumber(config: Record<string, unknown>, key: string, fallback: number): string {
  const value = config[key];
  return typeof value === "number" && Number.isFinite(value) ? String(value) : String(fallback);
}

export function emptyTriggerDraft(): TriggerDraft {
  return {
    name: "",
    kind: "webhook",
    action: "",
    credentialRef: "",
    webhookSecret: "",
    signatureHeader: "X-WF-Signature-256",
    deliveryIDHeader: "X-WF-Delivery-Id",
    correlationHeader: "X-WF-Correlation-Id",
    inputMode: "json",
    cron: "0 9 * * *",
    timezone: "Asia/Seoul",
    scheduleInput: "{}",
    rabbitMQURL: "",
    queue: "",
    prefetch: "1",
    concurrency: "1",
    consumerTag: "",
    completionMode: "poll",
    callbackEndpoint: "",
    callbackSigningSecret: "",
    publishRabbitMQURL: "",
    publishExchange: "",
    publishRoutingKey: "",
    responseMode: "async",
    responseTimeout: "30",
  };
}

export function draftFromTrigger(trigger: TriggerDefinition): TriggerDraft {
  const draft = emptyTriggerDraft();
  const config = trigger.config || {};
  return {
    ...draft,
    name: trigger.name,
    kind: trigger.kind,
    action: trigger.action,
    credentialRef: trigger.credential_ref || "",
    signatureHeader: configString(config, "signature_header", draft.signatureHeader),
    deliveryIDHeader: configString(config, "delivery_id_header", draft.deliveryIDHeader),
    correlationHeader: configString(config, "correlation_header", draft.correlationHeader),
    inputMode: configString(config, "input_mode", "json") === "raw" ? "raw" : "json",
    cron: configString(config, "cron", draft.cron),
    timezone: configString(config, "timezone", draft.timezone),
    scheduleInput: JSON.stringify(config.input ?? {}, null, 2),
    queue: configString(config, "queue"),
    prefetch: configNumber(config, "prefetch", 1),
    concurrency: configNumber(config, "concurrency", 1),
    consumerTag: configString(config, "consumer_tag"),
    completionMode: trigger.completion?.mode || "none",
    callbackEndpoint: trigger.completion?.callback?.endpoint || "",
    publishExchange: trigger.completion?.publish?.exchange || "",
    publishRoutingKey: trigger.completion?.publish?.routing_key || "",
    responseMode: trigger.response?.mode || "async",
    responseTimeout: String(trigger.response?.timeout_seconds || 30),
  };
}

export function buildTriggerPayload(
  draft: TriggerDraft,
  app: string,
  existing?: TriggerDefinition | null,
): TriggerPayloadResult {
  const name = draft.name.trim();
  const action = draft.action.trim();
  if (!name) return { error: "Name is required." };
  if (!action) return { error: "Select a target Action." };

  const payload: TriggerPayload = {
    name,
    kind: draft.kind,
    enabled: existing?.enabled ?? false,
    app,
    action,
    config: {},
    completion: { mode: draft.completionMode },
    response: { mode: draft.kind === "webhook" ? draft.responseMode : "async" },
  };
  if (draft.credentialRef.trim()) payload.credential_ref = draft.credentialRef.trim();

  const completionSecret: Record<string, string> = {};
  if (draft.completionMode === "callback") {
    if (!draft.callbackEndpoint.trim()) {
      return { error: "Callback endpoint is required." };
    }
    if (existing?.completion.mode !== "callback" && !draft.callbackSigningSecret.trim()) {
      return { error: "Callback signing secret is required." };
    }
    payload.completion = {
      mode: "callback",
      callback: { endpoint: draft.callbackEndpoint.trim() },
    };
    if (draft.callbackSigningSecret.trim()) {
      completionSecret.signing_secret = draft.callbackSigningSecret.trim();
    }
  } else if (draft.completionMode === "publish") {
    if (!draft.publishRoutingKey.trim()) {
      return { error: "Completion routing key is required." };
    }
    if (existing?.completion.mode !== "publish" && !draft.publishRabbitMQURL.trim()) {
      return { error: "Completion RabbitMQ URL is required." };
    }
    payload.completion = {
      mode: "publish",
      publish: {
        exchange: draft.publishExchange.trim() || undefined,
        routing_key: draft.publishRoutingKey.trim(),
      },
    };
    if (draft.publishRabbitMQURL.trim()) {
      completionSecret.rabbitmq_url = draft.publishRabbitMQURL.trim();
    }
  }
  if (draft.kind === "webhook" && draft.responseMode === "wait") {
    const timeout = Number(draft.responseTimeout);
    if (!Number.isInteger(timeout) || timeout < 1 || timeout > 60) {
      return { error: "Wait timeout must be an integer between 1 and 60 seconds." };
    }
    payload.response = { mode: "wait", timeout_seconds: timeout };
  }

  if (draft.kind === "webhook") {
    if (!existing && !draft.webhookSecret.trim()) {
      return { error: "Webhook signing secret is required." };
    }
    payload.config = {
      signature_header: draft.signatureHeader.trim() || "X-WF-Signature-256",
      delivery_id_header: draft.deliveryIDHeader.trim() || "X-WF-Delivery-Id",
      correlation_header: draft.correlationHeader.trim() || "X-WF-Correlation-Id",
      input_mode: draft.inputMode,
    };
    if (draft.webhookSecret.trim()) {
      payload.secret_config = { secret: draft.webhookSecret.trim() };
    }
    if (Object.keys(completionSecret).length > 0) {
      payload.secret_config = { ...(payload.secret_config || {}), completion: completionSecret };
    }
    return { payload };
  }

  if (draft.kind === "schedule") {
    if (!draft.cron.trim()) return { error: "Cron expression is required." };
    if (!draft.timezone.trim()) return { error: "Timezone is required." };
    let input: unknown;
    try {
      input = JSON.parse(draft.scheduleInput.trim() || "{}");
    } catch {
      return { error: "Schedule input must be valid JSON." };
    }
    payload.config = {
      cron: draft.cron.trim(),
      timezone: draft.timezone.trim(),
      input,
    };
    if (Object.keys(completionSecret).length > 0) {
      payload.secret_config = { completion: completionSecret };
    }
    return { payload };
  }

  if (!draft.queue.trim()) return { error: "RabbitMQ queue is required." };
  if (!existing && !draft.rabbitMQURL.trim()) {
    return { error: "RabbitMQ connection URL is required." };
  }
  const concurrency = Number(draft.concurrency);
  const prefetch = Number(draft.prefetch);
  if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 128) {
    return { error: "Concurrency must be an integer between 1 and 128." };
  }
  if (!Number.isInteger(prefetch) || prefetch < concurrency || prefetch > 65535) {
    return { error: "Prefetch must be an integer at least equal to concurrency." };
  }
  payload.config = {
    queue: draft.queue.trim(),
    prefetch,
    concurrency,
    input_mode: draft.inputMode,
  };
  if (draft.consumerTag.trim()) payload.config.consumer_tag = draft.consumerTag.trim();
  if (draft.deliveryIDHeader.trim()) {
    payload.config.delivery_id_header = draft.deliveryIDHeader.trim();
  }
  if (draft.rabbitMQURL.trim()) {
    payload.secret_config = { url: draft.rabbitMQURL.trim() };
  }
  if (Object.keys(completionSecret).length > 0) {
    payload.secret_config = { ...(payload.secret_config || {}), completion: completionSecret };
  }
  return { payload };
}

export function triggerKindLabel(kind: TriggerKind): string {
  if (kind === "rabbitmq") return "RabbitMQ";
  if (kind === "schedule") return "Schedule";
  return "Webhook";
}

export function triggerConfigSummary(trigger: TriggerDefinition): string {
  if (trigger.kind === "schedule") {
    return `${configString(trigger.config, "cron", "—")} · ${configString(trigger.config, "timezone", "—")}`;
  }
  if (trigger.kind === "rabbitmq") {
    return configString(trigger.config, "queue", "Queue not set");
  }
  return configString(trigger.config, "input_mode", "json") === "raw"
    ? "Raw request envelope"
    : "JSON request body";
}

export function httpRouteProvider(info: SystemInfo | null | undefined): string {
  if (!info?.backends.http_route_provider) return "";
  const provider = info.runtime_config.http_route_provider;
  return typeof provider === "string" ? provider.trim() : "";
}
