import { type TranslationKey, translate } from "../shared/i18n";

export type Settings = {
  workspace: string;
  token: string;
  actor: string;
};

export const defaultSettings: Settings = {
  workspace: "default",
  token: "",
  actor: "local-dev",
};

export function loadSettings(): Settings {
  const store = globalThis.localStorage;
  if (!store) return defaultSettings;
  // `??` keeps a deliberately cleared value ("") cleared across reloads;
  // only a missing key falls back to the default.
  return {
    workspace: store.getItem("wf.workspace") || defaultSettings.workspace,
    token: store.getItem("wf.token") ?? defaultSettings.token,
    actor: store.getItem("wf.actor") ?? defaultSettings.actor,
  };
}

export function saveSettings(settings: Settings) {
  const store = globalThis.localStorage;
  if (!store) return;
  store.setItem("wf.workspace", settings.workspace);
  store.setItem("wf.token", settings.token);
  store.setItem("wf.actor", settings.actor);
}

export type GitSource = {
  id: number;
  workspace_id: string;
  name: string;
  app_key?: string;
  repo_url: string;
  branch: string;
  subpath: string;
  creds_ref: string;
  kind: string;
  last_synced_commit?: string | null;
  last_synced_at?: string | null;
  created_at: string;
};

export type Client = {
  id: string;
  workspace_id: string;
  name: string;
  has_token: boolean;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
};

export type ClientPayload = {
  name: string;
};

export type ClientTokenResult = {
  client: Client;
  api_token: string;
};

export type InputConfig = {
  workspace_id: string;
  app_key: string;
  action_key: string;
  client_id?: string;
  config: Record<string, unknown>;
  locked_keys: string[];
  updated_by: string;
  updated_at: string;
};

export type InputConfigPayload = {
  action_key: string;
  client_id?: string;
  config: Record<string, unknown>;
  locked_keys: string[];
};

export type InputConfigAudit = {
  id: string;
  workspace_id: string;
  app_key: string;
  action_key: string;
  client_id?: string;
  kind: string;
  detail?: string;
  actor: string;
  created_at: string;
};

export type Variable = {
  app_key: string;
  path: string;
  value: string;
  is_secret: boolean;
  description: string;
};

export type SetVariablePayload = {
  app_key?: string;
  path: string;
  value: string;
  is_secret: boolean;
  description?: string;
};

export type Resource = {
  path: string;
  value: unknown;
  resource_type: string;
  description: string;
};

export type ResourcePayload = Resource;

export type ResourceType = {
  name: string;
  version: string;
  schema: Record<string, unknown>;
  description: string;
};

export type ProbeResult = {
  reachable: boolean;
  branch?: string;
  branch_exists?: boolean;
  branches?: string[];
  error?: string;
};

export type SourceSyncResult = {
  commit: string;
  app: string;
  actions: string[];
  runtime: string;
  sync_status: "synced";
  synced_at: string;
  validation_checks: string[];
};

export type DeployResult = {
  commit: string;
  app: string;
  actions: string[];
  source?: string;
  deployment_id?: string;
  created_by?: string;
  message?: string;
  bundle_status: "ready";
  bundle_digest: string;
  bundle_uri?: string;
  runtime: string;
  validation_checks: string[];
};

export type AppSummary = {
  id: string;
  workspace_id: string;
  app_key: string;
  git_source_id: number;
  commit_sha: string;
  entrypoint: string;
  tag: string;
  tag_override?: string;
  timeout_s: number;
  script_lang: string;
  bundle_status: "ready" | "missing";
  bundle_digest?: string;
  bundle_uri?: string;
  required_capabilities?: string[];
  max_concurrent?: number | null;
  updated_at: string;
  effective_route_tag: string;
  actions_count: number;
};

export type ActionView = {
  id: string;
  workspace_id: string;
  app_key: string;
  action_key: string;
  display_name?: string;
  input_schema?: string;
  output_schema?: string;
  tag?: string;
  tag_override?: string;
  timeout_s?: number;
  required_capabilities?: string[];
  runtime_access?: {
    variables?: string[];
    resources?: string[];
  };
  updated_at: string;
  effective_capabilities?: string[];
  effective_route_tag?: string;
};

export type AppDetail = {
  app: AppSummary;
  actions: ActionView[];
};

export type ActionSchemas = {
  workspace_id: string;
  app_key: string;
  action_key: string;
  input_schema: unknown;
  output_schema: unknown;
  operator_settings_schema: unknown;
};

export type AppDocumentation = {
  app_key: string;
  commit_sha: string;
  available: boolean;
  path?: string;
  markdown?: string;
};

export type HistoryItem = {
  id: string;
  commit_sha: string;
  entrypoint: string;
  source: string;
  active: boolean;
  bundle_status: "ready" | "missing";
  deployment_id?: string;
  message?: string;
  created_by?: string;
  created_at: string;
};

export type ReleaseRollbackResult = {
  app: string;
  active_release_id: string;
  previous_release_id: string;
  commit: string;
  bundle_digest: string;
  actor: string;
  reason: string;
  rolled_back_at: string;
};

export type JobStatusCounts = {
  queued_count: number;
  running_count: number;
  completed_count_recent: number;
  failed_count_recent: number;
  canceled_count_recent: number;
};

export type JobsSummary = JobStatusCounts & {
  oldest_queued_at?: string | null;
  by_tag?: Array<JobStatusCounts & { tag: string }>;
  by_app?: Array<JobStatusCounts & { app_key: string }>;
};

export type JobStatus = {
  id: string;
  workspace_id: string;
  state: "queued" | "running" | "succeeded" | "failed" | string;
  status?: string;
  worker?: string;
  app_key?: string;
  action_key?: string;
  trigger_kind?: string;
  commit_sha?: string;
  tag?: string;
  created_at?: string;
  started_at?: string;
  completed_at?: string;
  duration_ms?: number;
};

export type JobLogStreamEvent = {
  type: "update" | "ping" | "timeout" | "error" | "notfound";
  running?: boolean;
  completed?: boolean;
  new_logs?: string;
  log_offset?: number;
  status?: string;
  attempt?: number;
  worker_id?: string;
  error?: string;
};

export type JobLogStreamResult = {
  offset: number;
  completed: boolean;
  timedOut: boolean;
};

export type AuditRecord = {
  id: string;
  git_source_id: number;
  app_key?: string;
  kind: string;
  detail?: string;
  actor: string;
  created_at: string;
};

export type AuditChanges = {
  added?: string[];
  updated?: string[];
  removed?: string[];
  locked?: string[];
  unlocked?: string[];
};

export type AuditEvent = {
  id: string;
  category: "repository" | "release" | "client" | "input_settings" | string;
  kind: string;
  summary: string;
  detail?: string;
  app_key?: string;
  action_key?: string;
  client_id?: string;
  client_name?: string;
  git_source_id?: number;
  webhook_subscription_id?: string;
  webhook_delivery_id?: string;
  job_id?: string;
  attempt?: number;
  runtime_config_path?: string;
  source?: string;
  actor: string;
  changes?: AuditChanges;
  created_at: string;
};

export type WebhookSubscription = {
  id: string;
  workspace_id: string;
  name: string;
  endpoint_summary: string;
  has_signing_secret: boolean;
  event_types: string[] | null;
  app_keys: string[] | null;
  enabled: boolean;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string | null;
};

export type WebhookSubscriptionMutation = {
  subscription: WebhookSubscription;
  signing_secret?: string;
};

export type WebhookSubscriptionCreate = {
  name: string;
  endpoint: string;
  event_types?: string[];
  app_keys?: string[];
  enabled?: boolean;
};

export type WebhookSubscriptionUpdate = {
  name?: string;
  endpoint?: string;
  event_types?: string[];
  app_keys?: string[];
  enabled?: boolean;
  rotate_signing_secret?: boolean;
};

export function webhookAppKeys(subscription: WebhookSubscription): string[] {
  return subscription.app_keys || [];
}

export type ControlPlaneEvent = {
  specversion: string;
  id: string;
  type: string;
  source: string;
  subject: string;
  time: string;
  datacontenttype: string;
  data: Record<string, unknown>;
};

export type WebhookDeliveryState =
  | "pending"
  | "delivering"
  | "retrying"
  | "succeeded"
  | "failed"
  | "canceled";

export type WebhookDelivery = {
  id: string;
  workspace_id: string;
  event_id: string;
  subscription_id: string;
  state: WebhookDeliveryState;
  attempt: number;
  next_attempt_at: string;
  lease_owner?: string | null;
  lease_expires_at?: string | null;
  response_status?: number | null;
  latency_ms?: number | null;
  error_summary?: string | null;
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
};

export type WebhookDeliveryDetail = {
  delivery: WebhookDelivery;
  event: ControlPlaneEvent;
  subscription_name: string;
};

export type WebhookDeliveryPage = {
  items: WebhookDeliveryDetail[];
  next_cursor?: string;
};

export type ProvisioningAppliedResource = {
  kind: string;
  name: string;
  action: string;
  detail?: string;
};

export type ProvisioningImportResult = {
  applied: ProvisioningAppliedResource[];
};

export type SystemInfo = {
  service: string;
  workspace: string;
  ready: boolean;
  planes: Record<string, boolean>;
  backends: Record<string, boolean>;
  auth: Record<string, boolean>;
  runtime_config: Record<string, unknown>;
};

export type Workspace = {
  id: string;
  name: string;
  status: "active" | "archived";
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
};

export type WorkspaceToken = {
  id: string;
  workspace_id: string;
  name: string;
  status: "active" | "revoked";
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
  revoked_at?: string;
};

export type WorkspaceAudit = {
  id: string;
  workspace_id: string;
  kind: string;
  detail?: string;
  actor: string;
  created_at: string;
};

export type WorkspaceTokenResult = {
  token: WorkspaceToken;
  api_token: string;
};

export type WebhookDeliveryQuery = {
  state?: WebhookDeliveryState | "";
  limit?: number;
  cursor?: string;
};

export type TriggerKind = "webhook" | "schedule" | "rabbitmq";
export type TriggerCompletionMode = "none" | "poll" | "callback" | "publish";
export type TriggerResponseMode = "async" | "wait";

export type TriggerCompletionPolicy = {
  mode: TriggerCompletionMode;
  callback?: { endpoint: string };
  publish?: { exchange?: string; routing_key: string };
};

export type TriggerResponsePolicy = {
  mode: TriggerResponseMode;
  timeout_seconds?: number;
};

export type TriggerDefinition = {
  id: string;
  workspace_id: string;
  name: string;
  kind: TriggerKind;
  enabled: boolean;
  app: string;
  action: string;
  credential_ref?: string;
  config: Record<string, unknown>;
  completion: TriggerCompletionPolicy;
  response: TriggerResponsePolicy;
  has_secret: boolean;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
};

export type TriggerPayload = {
  name: string;
  kind: TriggerKind;
  enabled?: boolean;
  app: string;
  action: string;
  credential_ref?: string;
  config: Record<string, unknown>;
  completion: TriggerCompletionPolicy;
  response?: TriggerResponsePolicy;
  secret_config?: Record<string, unknown>;
};

export type TriggerDeliveryState = "admitted" | "retryable" | "terminal";

export type TriggerDelivery = {
  id: string;
  workspace_id: string;
  trigger_id: string;
  delivery_id: string;
  correlation_id?: string;
  state: TriggerDeliveryState;
  run_id?: string;
  attempt: number;
  error_summary?: string;
  scheduled_for?: string;
  completion: TriggerCompletionPolicy;
  completion_state:
    | "waiting"
    | "ignored"
    | "available"
    | "pending"
    | "delivering"
    | "retrying"
    | "succeeded"
    | "failed";
  completion_attempt: number;
  completion_next_attempt_at?: string;
  completion_response_status?: number;
  completion_error_summary?: string;
  completion_completed_at?: string;
  created_at: string;
  updated_at: string;
};

export type TriggerAudit = {
  id: string;
  workspace_id: string;
  trigger_id: string;
  kind: string;
  detail?: string;
  actor: string;
  created_at: string;
};

export type HTTPRouteBindingState = "pending" | "ready" | "error" | "deleting" | "deleted";

export type HTTPRouteBinding = {
  id: string;
  workspace_id: string;
  trigger_id: string;
  hostname?: string;
  path: string;
  visibility: "public";
  provider: string;
  state: HTTPRouteBindingState;
  public_url?: string;
  error_summary?: string;
  generation: number;
  observed_generation: number;
  created_by: string;
  updated_by: string;
  created_at: string;
  updated_at: string;
  delete_requested_at?: string;
  deleted_at?: string;
};

export type HTTPRouteBindingPayload = {
  hostname?: string;
  path: string;
  visibility?: "public";
  provider?: string;
};

export type HTTPRouteBindingAudit = {
  id: string;
  workspace_id: string;
  trigger_id: string;
  binding_id: string;
  kind: string;
  detail?: string;
  actor: string;
  created_at: string;
};

export type AuditEventQuery = {
  appKey?: string;
  clientID?: string;
  category?: string;
  actor?: string;
  gitSourceID?: number;
  since?: string;
  until?: string;
  limit?: number;
};

export type HumanTaskState = "pending" | "decided" | "expired" | "canceled";

export type HumanTask = {
  id: string;
  workspace_id: string;
  run_id: string;
  job_id: string;
  attempt: number;
  app?: string;
  action?: string;
  key: string;
  mode: "hold";
  kind: "form";
  state: HumanTaskState;
  title: string;
  description?: string;
  input_schema?: JSONSchema;
  presentation?: Record<string, unknown>;
  has_private_context: boolean;
  decision_outcome?: "submit" | "cancel";
  decided_by?: string;
  terminal_cause?: string;
  created_at: string;
  updated_at: string;
  decided_at?: string;
  expires_at?: string;
};

export type JSONSchema = {
  type?: string;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  properties?: Record<string, JSONSchema>;
  required?: string[];
};

export type HumanTaskDecisionPayload = {
  outcome: "submit" | "cancel";
  value?: Record<string, unknown>;
};

export type RegisterSourcePayload = {
  name: string;
  repo_url: string;
  branch?: string;
  subpath?: string;
  creds_ref?: string;
};

export type PatchSourcePayload = {
  name?: string;
  repo_url?: string;
  branch?: string;
  subpath?: string;
  creds_ref?: string;
};

export type ApiErrorCode =
  | "http.bad_request"
  | "http.unauthorized"
  | "http.forbidden"
  | "http.not_found"
  | "http.conflict"
  | "http.rate_limited"
  | "http.server_error"
  | `server.${string}`;

export class ApiError extends Error {
  constructor(
    readonly code: ApiErrorCode,
    readonly status: number,
    readonly detail: string,
    readonly data?: unknown,
  ) {
    super(detail);
    this.name = "ApiError";
  }
}

export function errorMessage(cause: unknown): string {
  if (cause instanceof ApiError) return translate(apiErrorTranslationKey(cause.status));
  if (cause instanceof TypeError) return translate("apiError.network");
  if (cause instanceof Error) return cause.message;
  return translate("apiError.unexpected");
}

function apiErrorTranslationKey(status: number): TranslationKey {
  if (status === 400 || status === 422) return "apiError.badRequest";
  if (status === 401) return "apiError.unauthorized";
  if (status === 403) return "apiError.forbidden";
  if (status === 404) return "apiError.notFound";
  if (status === 409) return "apiError.conflict";
  if (status === 429) return "apiError.rateLimited";
  return "apiError.server";
}

function isLegacyHeaderValueSafe(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code === 0x09) continue;
    if (code < 0x20 || code > 0x7e) return false;
  }
  return true;
}

function utf8Base64(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary);
}

export function setActorHeaders(headers: Headers, actor: string) {
  const subject = actor.trim();
  if (!subject) return;
  if (isLegacyHeaderValueSafe(subject)) {
    headers.set("x-windforce-actor", subject);
    return;
  }
  headers.set("x-windforce-actor-utf8", utf8Base64(subject));
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
};

export class WindforceApi {
  constructor(
    private readonly settings: Settings,
    private readonly onUnauthorized?: () => void,
  ) {}

  clients(): Promise<Client[]> {
    return this.request("/clients");
  }

  client(id: string): Promise<Client> {
    return this.request(`/clients/${encodeURIComponent(id)}`);
  }

  createClient(payload: ClientPayload): Promise<ClientTokenResult> {
    return this.request("/clients", { method: "POST", body: payload });
  }

  updateClient(id: string, payload: ClientPayload): Promise<Client> {
    return this.request(`/clients/${encodeURIComponent(id)}`, { method: "PATCH", body: payload });
  }

  async deleteClient(id: string): Promise<void> {
    await this.request(`/clients/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  rotateClientToken(id: string): Promise<ClientTokenResult> {
    return this.request(`/clients/${encodeURIComponent(id)}/token`, { method: "POST" });
  }

  revokeClientToken(id: string): Promise<Client> {
    return this.request(`/clients/${encodeURIComponent(id)}/token`, { method: "DELETE" });
  }

  clientInputConfigs(id: string): Promise<InputConfig[]> {
    return this.request(`/clients/${encodeURIComponent(id)}/input-configs`);
  }

  clientInputConfigAudit(id: string): Promise<InputConfigAudit[]> {
    return this.request(`/clients/${encodeURIComponent(id)}/input-config-audit`);
  }

  appInputConfigs(appKey: string): Promise<InputConfig[]> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/input-configs`);
  }

  appInputConfigAudit(appKey: string): Promise<InputConfigAudit[]> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/input-config-audit`);
  }

  setInputConfig(appKey: string, payload: InputConfigPayload): Promise<InputConfig> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/input-configs`, {
      method: "PUT",
      body: payload,
    });
  }

  async deleteInputConfig(appKey: string, actionKey: string, clientID = ""): Promise<void> {
    const params = new URLSearchParams();
    if (actionKey) params.set("action_key", actionKey);
    if (clientID) params.set("client_id", clientID);
    const query = params.toString();
    await this.request(
      `/apps/${encodeURIComponent(appKey)}/input-configs${query ? `?${query}` : ""}`,
      {
        method: "DELETE",
      },
    );
  }

  gitSources(): Promise<GitSource[]> {
    return this.request("/git_sources");
  }

  registerGitSource(payload: RegisterSourcePayload): Promise<GitSource> {
    return this.request("/git_sources", { method: "POST", body: payload });
  }

  probeGitSource(payload: Record<string, unknown>): Promise<ProbeResult> {
    return this.request("/git_sources/probe", { method: "POST", body: payload });
  }

  setVariable(payload: SetVariablePayload): Promise<{ path: string; app_key: string }> {
    return this.request("/variables", { method: "POST", body: payload });
  }

  variables(): Promise<Variable[]> {
    return this.request("/variables");
  }

  async deleteVariable(path: string, appKey = ""): Promise<void> {
    const params = new URLSearchParams();
    if (appKey) params.set("app", appKey);
    const query = params.toString();
    await this.request(`/variables/p/${encodePath(path)}${query ? `?${query}` : ""}`, {
      method: "DELETE",
    });
  }

  resources(): Promise<Resource[]> {
    return this.request("/resources");
  }

  setResource(payload: ResourcePayload): Promise<{ path: string }> {
    return this.request("/resources", { method: "POST", body: payload });
  }

  async deleteResource(path: string): Promise<void> {
    await this.request(`/resources/p/${encodePath(path)}`, { method: "DELETE" });
  }

  resourceTypes(): Promise<ResourceType[]> {
    return this.request("/resource-types");
  }

  setResourceType(payload: ResourceType): Promise<ResourceType> {
    return this.request("/resource-types", { method: "POST", body: payload });
  }

  async deleteResourceType(name: string, version: string): Promise<void> {
    await this.request(
      `/resource-types/${encodeURIComponent(name)}/${encodeURIComponent(version)}`,
      {
        method: "DELETE",
      },
    );
  }

  createSample(appKey: string): Promise<{ source: GitSource; sync_result: DeployResult }> {
    return this.request("/git_sources/sample", { method: "POST", body: { app_key: appKey } });
  }

  patchGitSource(id: number, payload: PatchSourcePayload): Promise<GitSource> {
    return this.request(`/git_sources/${id}`, { method: "PATCH", body: payload });
  }

  async deleteGitSource(id: number): Promise<void> {
    await this.request(`/git_sources/${id}`, { method: "DELETE" });
  }

  syncGitSource(id: number): Promise<SourceSyncResult> {
    return this.request(`/git_sources/${id}/sync`, { method: "POST" });
  }

  deployGitSource(id: number, message: string): Promise<DeployResult> {
    const body: Record<string, unknown> = { confirm: true };
    if (message) body.message = message;
    return this.request(`/git_sources/${id}/deploy`, { method: "POST", body });
  }

  apps(): Promise<{ apps: AppSummary[] }> {
    return this.request("/apps?view=summary");
  }

  app(appKey: string): Promise<AppDetail> {
    return this.request(`/apps/${encodeURIComponent(appKey)}`);
  }

  appHistory(appKey: string): Promise<HistoryItem[]> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/history`);
  }

  rollbackAppRelease(
    appKey: string,
    releaseID: string,
    reason: string,
  ): Promise<ReleaseRollbackResult> {
    return this.request(
      `/apps/${encodeURIComponent(appKey)}/releases/${encodeURIComponent(releaseID)}/rollback`,
      { method: "POST", body: { confirm: true, reason } },
    );
  }

  actionSchemas(appKey: string, actionKey: string): Promise<ActionSchemas> {
    return this.request(
      `/apps/${encodeURIComponent(appKey)}/actions/${encodeURIComponent(actionKey)}/schema`,
    );
  }

  appDocumentation(appKey: string): Promise<AppDocumentation> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/documentation`);
  }

  jobsSummary(recentSeconds = 86400): Promise<JobsSummary> {
    return this.request(`/jobs/summary?recent_seconds=${recentSeconds}`);
  }

  job(jobID: string): Promise<JobStatus> {
    return this.request(`/jobs/${encodeURIComponent(jobID)}`);
  }

  humanTasks(state = "", limit = 100): Promise<{ items: HumanTask[] }> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (state) params.set("state", state);
    return this.request(`/human-tasks?${params.toString()}`);
  }

  humanTask(id: string): Promise<HumanTask> {
    return this.request(`/human-tasks/${encodeURIComponent(id)}`);
  }

  decideHumanTask(
    id: string,
    payload: HumanTaskDecisionPayload,
    idempotencyKey: string = crypto.randomUUID(),
  ): Promise<{ task: HumanTask; replayed: boolean }> {
    return this.request(`/human-tasks/${encodeURIComponent(id)}/decision`, {
      method: "POST",
      body: payload,
      headers: { "idempotency-key": idempotencyKey },
    });
  }

  async streamJobLogs(
    jobID: string,
    options: {
      offset: number;
      timeoutSeconds?: number;
      signal?: AbortSignal;
      onEvent: (event: JobLogStreamEvent) => void;
    },
  ): Promise<JobLogStreamResult> {
    const params = new URLSearchParams({
      offset: String(options.offset),
      timeout_seconds: String(options.timeoutSeconds || 60),
    });
    const headers = new Headers({ accept: "text/event-stream" });
    if (this.settings.token) headers.set("authorization", `Bearer ${this.settings.token}`);
    setActorHeaders(headers, this.settings.actor);
    const url = this.workspaceURL(`/jobs/${encodeURIComponent(jobID)}/logs/stream?${params}`);
    const response = await fetch(url, { headers, signal: options.signal });
    if (!response.ok) {
      const text = await response.text();
      if (response.status === 401) this.onUnauthorized?.();
      throw apiError(response, text);
    }
    if (!response.body) {
      throw new ApiError("http.server_error", 500, "job log stream has no response body");
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let offset = options.offset;
    let completed = false;
    let timedOut = false;
    const consume = (blocks: string[]) => {
      for (const block of blocks) {
        const event = decodeJobLogSSEBlock(block);
        if (!event) continue;
        options.onEvent(event);
        if (event.type === "update") {
          offset = event.log_offset ?? offset;
          completed = event.completed === true;
        } else if (event.type === "timeout") {
          timedOut = true;
        } else if (event.type === "notfound") {
          throw new ApiError("http.not_found", 404, "job not found");
        } else if (event.type === "error") {
          throw new ApiError("http.server_error", 500, event.error || "job log stream failed");
        }
      }
    };

    while (true) {
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      const split = splitSSEBlocks(buffer);
      buffer = split.remainder;
      consume(split.blocks);
      if (done) break;
    }
    if (buffer.trim()) consume([buffer]);
    return { offset, completed, timedOut };
  }

  auditTrail(sourceID: number): Promise<AuditRecord[]> {
    return this.request(`/git_sources/${sourceID}/audit`);
  }

  auditEvents(query: AuditEventQuery = {}): Promise<AuditEvent[]> {
    const params = new URLSearchParams();
    if (query.appKey) params.set("app_key", query.appKey);
    if (query.clientID) params.set("client_id", query.clientID);
    if (query.category) params.set("category", query.category);
    if (query.actor) params.set("actor", query.actor);
    if (query.gitSourceID) params.set("git_source_id", String(query.gitSourceID));
    if (query.since) params.set("since", query.since);
    if (query.until) params.set("until", query.until);
    if (query.limit) params.set("limit", String(query.limit));
    const suffix = params.size ? `?${params.toString()}` : "";
    return this.request(`/audit-events${suffix}`);
  }

  webhookSubscriptions(includeDeleted = false): Promise<WebhookSubscription[]> {
    const suffix = includeDeleted ? "?include_deleted=true" : "";
    return this.request(`/webhooks${suffix}`);
  }

  webhookSubscription(id: string): Promise<WebhookSubscription> {
    return this.request(`/webhooks/${encodeURIComponent(id)}`);
  }

  createWebhookSubscription(
    payload: WebhookSubscriptionCreate,
  ): Promise<WebhookSubscriptionMutation> {
    return this.request("/webhooks", { method: "POST", body: payload });
  }

  updateWebhookSubscription(
    id: string,
    payload: WebhookSubscriptionUpdate,
  ): Promise<WebhookSubscriptionMutation> {
    return this.request(`/webhooks/${encodeURIComponent(id)}`, { method: "PATCH", body: payload });
  }

  async deleteWebhookSubscription(id: string): Promise<void> {
    await this.request(`/webhooks/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  testWebhookSubscription(id: string): Promise<WebhookDeliveryDetail> {
    return this.request(`/webhooks/${encodeURIComponent(id)}/test`, { method: "POST" });
  }

  webhookDeliveries(id: string, query: WebhookDeliveryQuery = {}): Promise<WebhookDeliveryPage> {
    const params = new URLSearchParams();
    if (query.state) params.set("state", query.state);
    if (query.limit) params.set("limit", String(query.limit));
    if (query.cursor) params.set("cursor", query.cursor);
    const suffix = params.size ? `?${params.toString()}` : "";
    return this.request(`/webhooks/${encodeURIComponent(id)}/deliveries${suffix}`);
  }

  webhookDelivery(id: string): Promise<WebhookDeliveryDetail> {
    return this.request(`/webhook-deliveries/${encodeURIComponent(id)}`);
  }

  retryWebhookDelivery(id: string): Promise<WebhookDeliveryDetail> {
    return this.request(`/webhook-deliveries/${encodeURIComponent(id)}/retry`, { method: "POST" });
  }

  triggers(): Promise<{ items: TriggerDefinition[] }> {
    return this.request("/triggers");
  }

  trigger(id: string): Promise<TriggerDefinition> {
    return this.request(`/triggers/${encodeURIComponent(id)}`);
  }

  createTrigger(payload: TriggerPayload): Promise<TriggerDefinition> {
    return this.request("/triggers", { method: "POST", body: payload });
  }

  updateTrigger(id: string, payload: TriggerPayload): Promise<TriggerDefinition> {
    return this.request(`/triggers/${encodeURIComponent(id)}`, { method: "PUT", body: payload });
  }

  setTriggerEnabled(id: string, enabled: boolean): Promise<TriggerDefinition> {
    return this.request(`/triggers/${encodeURIComponent(id)}/${enabled ? "enable" : "disable"}`, {
      method: "POST",
    });
  }

  async deleteTrigger(id: string): Promise<void> {
    await this.request(`/triggers/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  triggerDeliveries(id: string): Promise<{ items: TriggerDelivery[] }> {
    return this.request(`/triggers/${encodeURIComponent(id)}/deliveries`);
  }

  triggerAudit(id: string): Promise<{ items: TriggerAudit[] }> {
    return this.request(`/triggers/${encodeURIComponent(id)}/audit`);
  }

  httpRouteBindings(triggerID: string): Promise<{ items: HTTPRouteBinding[] }> {
    return this.request(`/triggers/${encodeURIComponent(triggerID)}/routes`);
  }

  createHTTPRouteBinding(
    triggerID: string,
    payload: HTTPRouteBindingPayload,
  ): Promise<HTTPRouteBinding> {
    return this.request(`/triggers/${encodeURIComponent(triggerID)}/routes`, {
      method: "POST",
      body: payload,
    });
  }

  updateHTTPRouteBinding(
    triggerID: string,
    bindingID: string,
    payload: HTTPRouteBindingPayload,
  ): Promise<HTTPRouteBinding> {
    return this.request(
      `/triggers/${encodeURIComponent(triggerID)}/routes/${encodeURIComponent(bindingID)}`,
      { method: "PUT", body: payload },
    );
  }

  deleteHTTPRouteBinding(triggerID: string, bindingID: string): Promise<HTTPRouteBinding> {
    return this.request(
      `/triggers/${encodeURIComponent(triggerID)}/routes/${encodeURIComponent(bindingID)}`,
      { method: "DELETE" },
    );
  }

  httpRouteBindingAudit(
    triggerID: string,
    bindingID: string,
  ): Promise<{ items: HTTPRouteBindingAudit[] }> {
    return this.request(
      `/triggers/${encodeURIComponent(triggerID)}/routes/${encodeURIComponent(bindingID)}/audit`,
    );
  }

  importProvisioning(
    text: string,
    dryRun: boolean,
    format: "yaml" | "json",
  ): Promise<ProvisioningImportResult> {
    const suffix = dryRun ? "?dry_run=true" : "";
    return this.requestRaw(`/provisioning/import${suffix}`, {
      method: "POST",
      body: text,
      contentType: format === "yaml" ? "application/yaml" : "application/json",
    });
  }

  exportProvisioning(format: "yaml" | "json", includeValues = false): Promise<string> {
    const params = new URLSearchParams();
    params.set("format", format);
    if (includeValues) params.set("include_values", "true");
    return this.requestText(`/provisioning/export?${params.toString()}`);
  }

  systemInfo(): Promise<SystemInfo> {
    return this.request("/system/info");
  }

  workspaces(): Promise<{ items: Workspace[] }> {
    return this.globalRequest("/api/workspaces");
  }

  workspace(id: string): Promise<Workspace> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}`);
  }

  createWorkspace(id: string, name: string): Promise<Workspace> {
    return this.globalRequest("/api/workspaces", { method: "POST", body: { id, name } });
  }

  updateWorkspace(id: string, name: string): Promise<Workspace> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: { name },
    });
  }

  archiveWorkspace(id: string): Promise<Workspace> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}/archive`, {
      method: "POST",
    });
  }

  deleteWorkspace(id: string): Promise<void> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  }

  workspaceTokens(id: string): Promise<{ items: WorkspaceToken[] }> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}/tokens`);
  }

  createWorkspaceToken(id: string, name: string): Promise<WorkspaceTokenResult> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}/tokens`, {
      method: "POST",
      body: { name },
    });
  }

  rotateWorkspaceToken(id: string, tokenID: string): Promise<WorkspaceTokenResult> {
    return this.globalRequest(
      `/api/workspaces/${encodeURIComponent(id)}/tokens/${encodeURIComponent(tokenID)}/rotate`,
      {
        method: "POST",
      },
    );
  }

  revokeWorkspaceToken(id: string, tokenID: string): Promise<WorkspaceToken> {
    return this.globalRequest(
      `/api/workspaces/${encodeURIComponent(id)}/tokens/${encodeURIComponent(tokenID)}`,
      {
        method: "DELETE",
      },
    );
  }

  workspaceAudit(id: string): Promise<{ items: WorkspaceAudit[] }> {
    return this.globalRequest(`/api/workspaces/${encodeURIComponent(id)}/audit`);
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    return this.requestURL(this.workspaceURL(path), options);
  }

  private async globalRequest<T>(path: string, options: RequestOptions = {}): Promise<T> {
    return this.requestURL(path, options);
  }

  private async requestURL<T>(url: string, options: RequestOptions = {}): Promise<T> {
    const headers = new Headers();
    headers.set("accept", "application/json");
    if (this.settings.token) headers.set("authorization", `Bearer ${this.settings.token}`);
    setActorHeaders(headers, this.settings.actor);
    for (const [name, value] of Object.entries(options.headers || {})) {
      headers.set(name, value);
    }
    let body: BodyInit | undefined;
    if (options.body !== undefined) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(options.body);
    }
    const response = await fetch(url, {
      method: options.method || "GET",
      headers,
      body,
    });
    const text = await response.text();
    if (!response.ok) {
      if (response.status === 401) this.onUnauthorized?.();
      throw apiError(response, text);
    }
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      return text as T;
    }
  }

  private async requestRaw<T>(
    path: string,
    options: { method: string; body: string; contentType: string },
  ): Promise<T> {
    const headers = new Headers();
    headers.set("accept", "application/json");
    headers.set("content-type", options.contentType);
    if (this.settings.token) headers.set("authorization", `Bearer ${this.settings.token}`);
    setActorHeaders(headers, this.settings.actor);
    const response = await fetch(this.workspaceURL(path), {
      method: options.method,
      headers,
      body: options.body,
    });
    const text = await response.text();
    if (!response.ok) {
      if (response.status === 401) this.onUnauthorized?.();
      throw apiError(response, text);
    }
    if (!text) return undefined as T;
    return JSON.parse(text) as T;
  }

  private async requestText(path: string): Promise<string> {
    const headers = new Headers();
    headers.set("accept", "application/yaml, application/json");
    if (this.settings.token) headers.set("authorization", `Bearer ${this.settings.token}`);
    setActorHeaders(headers, this.settings.actor);
    const response = await fetch(this.workspaceURL(path), { headers });
    const text = await response.text();
    if (!response.ok) {
      if (response.status === 401) this.onUnauthorized?.();
      throw apiError(response, text);
    }
    return text;
  }

  private workspaceURL(path: string): string {
    const workspace = encodeURIComponent(this.settings.workspace || "default");
    return `/api/w/${workspace}${path}`;
  }
}

export function splitSSEBlocks(buffer: string): { blocks: string[]; remainder: string } {
  const parts = buffer.split(/\r?\n\r?\n/);
  return { blocks: parts.slice(0, -1), remainder: parts.at(-1) || "" };
}

export function decodeJobLogSSEBlock(block: string): JobLogStreamEvent | null {
  const data = block
    .split(/\r?\n/)
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trimStart())
    .join("\n");
  if (!data) return null;
  return JSON.parse(data) as JobLogStreamEvent;
}

function apiError(response: Response, text: string): ApiError {
  let detail = `${response.status} ${response.statusText}`;
  let data: unknown;
  let serverCode = "";
  try {
    data = JSON.parse(text) as unknown;
    if (data && typeof data === "object") {
      const payload = data as {
        code?: unknown;
        message?: unknown;
        error?: unknown;
      };
      if (typeof payload.code === "string") serverCode = payload.code;
      if (typeof payload.message === "string") detail = payload.message;
      if (typeof payload.error === "string") detail = payload.error;
      if (payload.error && typeof payload.error === "object") {
        const nested = payload.error as { code?: unknown; message?: unknown };
        if (typeof nested.code === "string") serverCode = nested.code;
        if (typeof nested.message === "string") detail = nested.message;
      }
    }
  } catch {
    if (text) detail = text;
  }
  const code = serverCode ? (`server.${serverCode}` as const) : httpErrorCode(response.status);
  return new ApiError(code, response.status, detail, data);
}

function httpErrorCode(status: number): ApiErrorCode {
  if (status === 400 || status === 422) return "http.bad_request";
  if (status === 401) return "http.unauthorized";
  if (status === 403) return "http.forbidden";
  if (status === 404) return "http.not_found";
  if (status === 409) return "http.conflict";
  if (status === 429) return "http.rate_limited";
  return "http.server_error";
}

function encodePath(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join("/");
}
