import { describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import {
  ApiError,
  decodeJobLogSSEBlock,
  errorMessage,
  setActorHeaders,
  splitSSEBlocks,
  type WebhookSubscription,
  WindforceApi,
  webhookAppKeys,
} from "./api";

function decodeUTF8Base64(value: string): string {
  const binary = atob(value);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

describe("setActorHeaders", () => {
  test("keeps ASCII actors on the legacy header", () => {
    const headers = new Headers();

    setActorHeaders(headers, " local-dev ");

    expect(headers.get("x-windforce-actor")).toBe("local-dev");
    expect(headers.get("x-windforce-actor-utf8")).toBeNull();
  });

  test("encodes Unicode actors into an ASCII-safe header", () => {
    const headers = new Headers();

    setActorHeaders(headers, "홍길동");

    expect(headers.get("x-windforce-actor")).toBeNull();
    const encoded = headers.get("x-windforce-actor-utf8");
    expect(encoded).toBeTruthy();
    expect(decodeUTF8Base64(encoded || "")).toBe("홍길동");
  });
});

describe("localized API errors", () => {
  test("keeps stable code and detail data separate from localized presentation", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          code: "workspace_archived",
          error: "workspace archived",
          workspace_id: "team-a",
        }),
        { status: 403, statusText: "Forbidden" },
      )) as unknown as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "team-a", token: "", actor: "operator" });
      const cause = await api.apps().catch((error: unknown) => error);
      expect(cause).toBeInstanceOf(ApiError);
      expect(cause).toMatchObject({
        code: "server.workspace_archived",
        status: 403,
        detail: "workspace archived",
      });

      await setLocale("ko");
      expect(errorMessage(cause)).toBe("이 작업을 수행할 권한이 없습니다.");
      expect(errorMessage(cause)).not.toContain("workspace archived");
    } finally {
      await setLocale("en");
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi job log streaming", () => {
  test("keeps incomplete SSE blocks and decodes multiline data", () => {
    const split = splitSSEBlocks(
      'data: {"type":"ping"}\r\n\r\ndata: {"type":"update",\ndata: "log_offset":4}',
    );
    expect(split.blocks).toEqual(['data: {"type":"ping"}']);
    expect(split.remainder).toContain('"log_offset":4');
    expect(decodeJobLogSSEBlock(`${split.remainder}\n\n`)).toEqual({
      type: "update",
      log_offset: 4,
    });
  });

  test("authenticates, replays from a byte offset, and returns the terminal cursor", async () => {
    const originalFetch = globalThis.fetch;
    const calls: Array<{ url: string; headers: Headers }> = [];
    globalThis.fetch = (async (input, init) => {
      calls.push({ url: String(input), headers: new Headers(init?.headers) });
      return new Response(
        [
          'data: {"type":"update","new_logs":"hello ","log_offset":6,"completed":false}',
          "",
          'data: {"type":"update","new_logs":"world","log_offset":11,"completed":true}',
          "",
        ].join("\n"),
        { status: 200, headers: { "content-type": "text/event-stream" } },
      );
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "ops", token: "secret", actor: "operator" });
      const events: string[] = [];
      const result = await api.streamJobLogs("job/1", {
        offset: 0,
        timeoutSeconds: 30,
        onEvent: (event) => events.push(event.new_logs || ""),
      });
      expect(result).toEqual({ offset: 11, completed: true, timedOut: false });
      expect(events.join("")).toBe("hello world");
      expect(calls[0]?.url).toBe("/api/w/ops/jobs/job%2F1/logs/stream?offset=0&timeout_seconds=30");
      expect(calls[0]?.headers.get("accept")).toBe("text/event-stream");
      expect(calls[0]?.headers.get("authorization")).toBe("Bearer secret");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi release flow", () => {
  test("syncs without publishing and deploys the latest synchronized source", async () => {
    const requests: Array<{ url: string; method: string; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        body: String(init?.body || ""),
      });
      return new Response(JSON.stringify({ app: "echo", commit: "commit-a", actions: [] }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "default", token: "", actor: "operator" });
      await api.syncGitSource(7);
      await api.deployGitSource(7, "Release note");

      expect(requests).toEqual([
        {
          url: "/api/w/default/git_sources/7/sync",
          method: "POST",
          body: "",
        },
        {
          url: "/api/w/default/git_sources/7/deploy",
          method: "POST",
          body: JSON.stringify({ confirm: true, message: "Release note" }),
        },
      ]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("rolls back by release ID with an explicit reason", async () => {
    const requests: Array<{ url: string; method: string; headers: Headers; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        headers: new Headers(init?.headers),
        body: String(init?.body || ""),
      });
      return new Response(JSON.stringify({ active_release_id: "release-a", commit: "commit-a" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "default", token: "", actor: "operator" });
      await api.rollbackAppRelease("echo", "release/a", "Restore stable release");
      expect(requests).toHaveLength(1);
      expect(requests[0]?.url).toBe("/api/w/default/apps/echo/releases/release%2Fa/rollback");
      expect(requests[0]?.method).toBe("POST");
      expect(requests[0]?.headers.get("x-windforce-actor")).toBe("operator");
      expect(JSON.parse(requests[0]!.body)).toEqual({
        confirm: true,
        reason: "Restore stable release",
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi runtime configuration", () => {
  test("uses canonical workspace paths and preserves slash-separated keys", async () => {
    const requests: Array<{ url: string; method: string; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        body: String(init?.body || ""),
      });
      const url = String(input);
      const responseBody =
        url.endsWith("/variables") || url.endsWith("/resources") || url.endsWith("/resource-types")
          ? []
          : {};
      return new Response(JSON.stringify(responseBody), { status: 200 });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "ops", token: "", actor: "operator" });
      await api.variables();
      await api.resources();
      await api.resourceTypes();
      await api.deleteVariable("credentials/api key", "orders");
      await api.deleteResource("database/main");
      await api.deleteResourceType("database", "1");

      expect(requests.map(({ url, method }) => [url, method])).toEqual([
        ["/api/w/ops/variables", "GET"],
        ["/api/w/ops/resources", "GET"],
        ["/api/w/ops/resource-types", "GET"],
        ["/api/w/ops/variables/p/credentials/api%20key?app=orders", "DELETE"],
        ["/api/w/ops/resources/p/database/main", "DELETE"],
        ["/api/w/ops/resource-types/database/1", "DELETE"],
      ]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi webhooks", () => {
  test("treats a null app scope from the API as all apps", () => {
    expect(webhookAppKeys({ app_keys: null } as WebhookSubscription)).toEqual([]);
  });

  test("lists deleted subscriptions only when requested", async () => {
    const calls: string[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input) => {
      calls.push(String(input));
      return new Response("[]", { status: 200 });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "demo", token: "", actor: "operator" });
      await api.webhookSubscriptions();
      await api.webhookSubscriptions(true);
      expect(calls).toEqual(["/api/w/demo/webhooks", "/api/w/demo/webhooks?include_deleted=true"]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("creates a scoped release subscription with the actor header", async () => {
    const requests: Array<{ url: string; method: string; headers: Headers; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        headers: new Headers(init?.headers),
        body: String(init?.body || ""),
      });
      return new Response(
        JSON.stringify({ subscription: { id: "wh_1" }, signing_secret: "shown-once" }),
        {
          status: 201,
          headers: { "content-type": "application/json" },
        },
      );
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "default", token: "token", actor: "운영자" });
      await api.createWebhookSubscription({
        name: "Release notifications",
        endpoint: "https://hooks.example.test/releases",
        event_types: ["windforce.release.published"],
        app_keys: ["echo"],
        enabled: true,
      });
      expect(requests).toHaveLength(1);
      expect(requests[0]?.url).toBe("/api/w/default/webhooks");
      expect(requests[0]?.method).toBe("POST");
      expect(decodeUTF8Base64(requests[0]?.headers.get("x-windforce-actor-utf8") || "")).toBe(
        "운영자",
      );
      expect(JSON.parse(requests[0]!.body)).toEqual({
        name: "Release notifications",
        endpoint: "https://hooks.example.test/releases",
        event_types: ["windforce.release.published"],
        app_keys: ["echo"],
        enabled: true,
      });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("encodes delivery filters and retry identifiers", async () => {
    const calls: Array<{ url: string; method: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      calls.push({ url: String(input), method: init?.method || "GET" });
      const body = String(input).endsWith("/retry") ? { delivery: {}, event: {} } : { items: [] };
      return new Response(JSON.stringify(body), { status: 200 });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "default", token: "", actor: "operator" });
      await api.webhookDeliveries("wh/1", { state: "failed", limit: 25, cursor: "time/id" });
      await api.retryWebhookDelivery("delivery/1");
      expect(calls).toEqual([
        {
          url: "/api/w/default/webhooks/wh%2F1/deliveries?state=failed&limit=25&cursor=time%2Fid",
          method: "GET",
        },
        { url: "/api/w/default/webhook-deliveries/delivery%2F1/retry", method: "POST" },
      ]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi Triggers", () => {
  test("uses workspace Control API routes for the Trigger lifecycle", async () => {
    const requests: Array<{ url: string; method: string; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        body: String(init?.body || ""),
      });
      const body =
        String(input).endsWith("/deliveries") || String(input).endsWith("/audit")
          ? { items: [] }
          : String(input).endsWith("/triggers")
            ? { items: [] }
            : { id: "trg_1", name: "Daily" };
      return new Response(JSON.stringify(body), { status: 200 });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "ops", token: "", actor: "operator" });
      const payload = {
        name: "Daily",
        kind: "schedule" as const,
        app: "orders",
        action: "reconcile",
        config: { cron: "0 9 * * *", timezone: "Asia/Seoul", input: {} },
        completion: { mode: "poll" as const },
      };
      await api.triggers();
      await api.createTrigger(payload);
      await api.updateTrigger("trg/1", payload);
      await api.setTriggerEnabled("trg/1", true);
      await api.triggerDeliveries("trg/1");
      await api.triggerAudit("trg/1");
      const routePayload = {
        hostname: "hooks.example.com",
        path: "/orders",
        visibility: "public" as const,
        provider: "kubernetes-gateway-api",
      };
      await api.httpRouteBindings("trg/1");
      await api.createHTTPRouteBinding("trg/1", routePayload);
      await api.updateHTTPRouteBinding("trg/1", "hrb/1", routePayload);
      await api.httpRouteBindingAudit("trg/1", "hrb/1");
      await api.deleteHTTPRouteBinding("trg/1", "hrb/1");
      await api.deleteTrigger("trg/1");

      expect(requests.map(({ url, method }) => [url, method])).toEqual([
        ["/api/w/ops/triggers", "GET"],
        ["/api/w/ops/triggers", "POST"],
        ["/api/w/ops/triggers/trg%2F1", "PUT"],
        ["/api/w/ops/triggers/trg%2F1/enable", "POST"],
        ["/api/w/ops/triggers/trg%2F1/deliveries", "GET"],
        ["/api/w/ops/triggers/trg%2F1/audit", "GET"],
        ["/api/w/ops/triggers/trg%2F1/routes", "GET"],
        ["/api/w/ops/triggers/trg%2F1/routes", "POST"],
        ["/api/w/ops/triggers/trg%2F1/routes/hrb%2F1", "PUT"],
        ["/api/w/ops/triggers/trg%2F1/routes/hrb%2F1/audit", "GET"],
        ["/api/w/ops/triggers/trg%2F1/routes/hrb%2F1", "DELETE"],
        ["/api/w/ops/triggers/trg%2F1", "DELETE"],
      ]);
      expect(JSON.parse(requests[1]!.body)).toEqual(payload);
      expect(JSON.parse(requests[8]!.body)).toEqual(routePayload);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi provisioning", () => {
  test("imports raw YAML with dry-run and actor headers", async () => {
    const requests: Array<{ url: string; method: string; headers: Headers; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        headers: new Headers(init?.headers),
        body: String(init?.body || ""),
      });
      return new Response(
        JSON.stringify({ applied: [{ kind: "Client", name: "Client A", action: "validated" }] }),
        {
          status: 200,
        },
      );
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "default", token: "tok", actor: "operator" });
      const result = await api.importProvisioning(
        "kind: Client\nmetadata:\n  name: Client A\n",
        true,
        "yaml",
      );

      expect(result.applied[0]?.action).toBe("validated");
      expect(requests).toHaveLength(1);
      expect(requests[0]?.url).toBe("/api/w/default/provisioning/import?dry_run=true");
      expect(requests[0]?.method).toBe("POST");
      expect(requests[0]?.headers.get("content-type")).toBe("application/yaml");
      expect(requests[0]?.headers.get("authorization")).toBe("Bearer tok");
      expect(requests[0]?.headers.get("x-windforce-actor")).toBe("operator");
      expect(requests[0]?.body).toContain("kind: Client");
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  test("exports provisioning as raw text", async () => {
    const calls: string[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input) => {
      calls.push(String(input));
      return new Response("resources: []\n", {
        status: 200,
        headers: { "content-type": "application/yaml" },
      });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "ops", token: "", actor: "operator" });
      const text = await api.exportProvisioning("yaml", true);
      expect(text).toBe("resources: []\n");
      expect(calls).toEqual(["/api/w/ops/provisioning/export?format=yaml&include_values=true"]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi system info", () => {
  test("loads safe service information for the current workspace", async () => {
    const calls: string[] = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input) => {
      calls.push(String(input));
      return new Response(
        JSON.stringify({
          service: "windforce-lite",
          workspace: "ops",
          ready: true,
          planes: { control_api: true },
          backends: { state_store: true },
          auth: { admin_token_configured: true },
          runtime_config: { wait_ms: 250 },
        }),
        { status: 200 },
      );
    }) as typeof fetch;
    try {
      const api = new WindforceApi({ workspace: "ops", token: "", actor: "operator" });
      const info = await api.systemInfo();
      expect(info.ready).toBe(true);
      expect(info.auth.admin_token_configured).toBe(true);
      expect(calls).toEqual(["/api/w/ops/system/info"]);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});

describe("WindforceApi workspaces", () => {
  test("uses global management routes and preserves admin headers", async () => {
    const requests: Array<{ url: string; method: string; headers: Headers; body: string }> = [];
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      requests.push({
        url: String(input),
        method: init?.method || "GET",
        headers: new Headers(init?.headers),
        body: String(init?.body || ""),
      });
      return new Response(JSON.stringify({ items: [] }), { status: 200 });
    }) as typeof fetch;
    try {
      const api = new WindforceApi({
        workspace: "default",
        token: "admin-token",
        actor: "operator",
      });
      await api.workspaces();
      await api.workspace("team-a");
      await api.createWorkspace("team-a", "Team A");

      expect(requests.map((request) => [request.url, request.method])).toEqual([
        ["/api/workspaces", "GET"],
        ["/api/workspaces/team-a", "GET"],
        ["/api/workspaces", "POST"],
      ]);
      expect(requests[0]?.headers.get("authorization")).toBe("Bearer admin-token");
      expect(requests[2]?.headers.get("x-windforce-actor")).toBe("operator");
      expect(JSON.parse(requests[2]!.body)).toEqual({ id: "team-a", name: "Team A" });
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
