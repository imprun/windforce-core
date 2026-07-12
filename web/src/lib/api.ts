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
  return {
    workspace: store.getItem("wf.workspace") || defaultSettings.workspace,
    token: store.getItem("wf.token") || defaultSettings.token,
    actor: store.getItem("wf.actor") || defaultSettings.actor,
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
  repo_url: string;
  branch: string;
  subpath: string;
  creds_ref: string;
  kind: string;
  last_synced_commit?: string | null;
  last_synced_at?: string | null;
  created_at: string;
};

export type ProbeResult = {
  reachable: boolean;
  branch?: string;
  branch_exists?: boolean;
  branches?: string[];
  error?: string;
};

export type SyncResult = {
  commit: string;
  app: string;
  actions: string[];
  source?: string;
  deployment_id?: string;
  created_by?: string;
  message?: string;
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
  input_schema?: string;
  output_schema?: string;
  tag?: string;
  tag_override?: string;
  timeout_s?: number;
  required_capabilities?: string[];
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
};

export type HistoryItem = {
  id: string;
  commit_sha: string;
  entrypoint: string;
  source: string;
  deployment_id?: string;
  message?: string;
  created_by?: string;
  created_at: string;
};

export type AppSource = {
  app_key: string;
  git_source_id: number;
  commit_sha: string;
  files: Record<string, string>;
  skipped?: string[];
};

export type JobListItem = {
  id: string;
  workspace_id: string;
  app_key: string;
  action_key: string;
  trigger_kind: string;
  status: string;
  queued: boolean;
  running: boolean;
  completed: boolean;
  created_at: string;
  started_at?: string | null;
  completed_at?: string | null;
  duration_ms: number;
  worker?: string | null;
  git_source_id?: number | null;
  commit_sha?: string | null;
  entrypoint: string;
  tag: string;
  created_by: string;
  permissioned_as: string;
  canceled_by?: string | null;
  canceled_reason?: string | null;
  error_snippet?: string;
};

export type JobsResponse = {
  items: JobListItem[];
  pagination: {
    limit: number;
    count: number;
    has_more: boolean;
    next_cursor?: string;
  };
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

export type JobDetail = {
  id: string;
  workspace_id: string;
  state: "queued" | "running" | "completed";
  status?: "success" | "failure" | "canceled";
  worker?: string;
  app_key?: string;
  action_key?: string;
  trigger_kind?: string;
  kind?: string;
  git_source_id?: number;
  commit_sha?: string;
  entrypoint?: string;
  tag?: string;
  timeout_s?: number;
  created_by?: string;
  permissioned_as?: string;
  input?: unknown;
  created_at?: string;
  started_at?: string;
  completed_at?: string;
  duration_ms?: number;
  canceled_by?: string;
  canceled_reason?: string;
};

export type JobResult =
  | { status: "pending" }
  | { status: "success" | "failure" | "canceled"; result: unknown };

export type RunWaitResult = {
  job_id: string;
  status: "pending" | "success" | "failure" | "canceled";
  result?: unknown;
};

export type CancelResult = {
  found: boolean;
  completed_now: boolean;
  soft_canceled: boolean;
  already_completed: boolean;
};

export type WorkerTags = {
  tags?: Array<{
    tag: string;
    live_workers: number;
    capabilities?: string[];
  }>;
  dedicated_tag?: string | null;
};

export type RegisterSourcePayload = {
  name: string;
  repo_url: string;
  branch?: string;
  subpath?: string;
  creds_ref?: string;
  auth_method?: string;
  access_token?: string;
  username?: string;
  password?: string;
};

export type PatchSourcePayload = {
  name?: string;
  repo_url?: string;
  branch?: string;
  subpath?: string;
  creds_ref?: string;
};

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

type RequestOptions = {
  method?: string;
  body?: unknown;
  text?: boolean;
};

export class WindforceApi {
  constructor(private readonly settings: Settings) {}

  gitSources(): Promise<GitSource[]> {
    return this.request("/git_sources");
  }

  registerGitSource(payload: RegisterSourcePayload): Promise<GitSource> {
    return this.request("/git_sources", { method: "POST", body: payload });
  }

  probeGitSource(payload: Record<string, unknown>): Promise<ProbeResult> {
    return this.request("/git_sources/probe", { method: "POST", body: payload });
  }

  createSample(appKey: string): Promise<{ source: GitSource; sync_result: SyncResult }> {
    return this.request("/git_sources/sample", { method: "POST", body: { app_key: appKey } });
  }

  patchGitSource(id: number, payload: PatchSourcePayload): Promise<GitSource> {
    return this.request(`/git_sources/${id}`, { method: "PATCH", body: payload });
  }

  async deleteGitSource(id: number): Promise<void> {
    await this.request(`/git_sources/${id}`, { method: "DELETE" });
  }

  deployGitSource(id: number, message: string): Promise<SyncResult> {
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

  appSource(appKey: string): Promise<AppSource> {
    return this.request(`/apps/${encodeURIComponent(appKey)}/source`);
  }

  actionSchemas(appKey: string, actionKey: string): Promise<ActionSchemas> {
    return this.request(
      `/apps/${encodeURIComponent(appKey)}/actions/${encodeURIComponent(actionKey)}/schema`,
    );
  }

  runAndWait(appKey: string, actionKey: string, input: unknown, timeoutMs: number): Promise<RunWaitResult> {
    return this.request(
      `/jobs/run/${encodeURIComponent(appKey)}/${encodeURIComponent(actionKey)}/wait?timeout_ms=${timeoutMs}`,
      { method: "POST", body: input },
    );
  }

  jobs(params: { status?: string; app?: string; limit?: number; cursor?: string }): Promise<JobsResponse> {
    const query = new URLSearchParams();
    if (params.status && params.status !== "all") query.set("status", params.status);
    if (params.app) query.set("app", params.app);
    query.set("limit", String(params.limit ?? 50));
    if (params.cursor) query.set("cursor", params.cursor);
    return this.request(`/jobs?${query.toString()}`);
  }

  jobsSummary(): Promise<JobsSummary> {
    return this.request("/jobs/summary");
  }

  job(jobID: string): Promise<JobDetail> {
    return this.request(`/jobs/${encodeURIComponent(jobID)}`);
  }

  jobResult(jobID: string): Promise<JobResult> {
    return this.request(`/jobs/${encodeURIComponent(jobID)}/result`);
  }

  jobLogs(jobID: string, tailBytes = 65536): Promise<string> {
    return this.request(`/jobs/${encodeURIComponent(jobID)}/logs?tail_bytes=${tailBytes}`, { text: true });
  }

  cancelJob(jobID: string, reason: string): Promise<CancelResult> {
    return this.request(`/jobs/${encodeURIComponent(jobID)}/cancel`, {
      method: "POST",
      body: reason ? { reason } : {},
    });
  }

  workerTags(): Promise<WorkerTags> {
    return this.request("/worker-tags");
  }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    const headers = new Headers();
    headers.set("accept", options.text ? "text/plain" : "application/json");
    if (this.settings.token) headers.set("authorization", `Bearer ${this.settings.token}`);
    if (this.settings.actor) headers.set("x-windforce-actor", this.settings.actor);
    let body: BodyInit | undefined;
    if (options.body !== undefined) {
      headers.set("content-type", "application/json");
      body = JSON.stringify(options.body);
    }
    const workspace = encodeURIComponent(this.settings.workspace || "default");
    const response = await fetch(`/api/w/${workspace}${path}`, {
      method: options.method || "GET",
      headers,
      body,
    });
    const text = await response.text();
    if (!response.ok) {
      let message = `${response.status} ${response.statusText}`;
      try {
        const payload = JSON.parse(text) as { error?: string };
        if (payload?.error) message = payload.error;
      } catch {
        if (text) message = text;
      }
      throw new ApiError(message, response.status);
    }
    if (options.text) return text as T;
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      return text as T;
    }
  }
}
