import { spawn } from "node:child_process";
import { createHmac } from "node:crypto";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { createServer as createHttpServer } from "node:http";
import path from "node:path";

function envValue(name, fallback = "") {
  const value = process.env[name]?.trim();
  if (value) return value;
  const corePrefix = "WINDFORCE_CORE_";
  if (name.startsWith(corePrefix)) {
    const legacyName = `WINDFORCE_LITE_${name.slice(corePrefix.length)}`;
    const legacyValue = process.env[legacyName]?.trim();
    if (legacyValue) return legacyValue;
  }
  return fallback;
}

const port = Number(envValue("WINDFORCE_CORE_UI_GUIDE_PORT", "18099"));
const external = envValue("WINDFORCE_CORE_UI_GUIDE_EXTERNAL") === "true";
const baseDir = path.resolve(".tmp/ui-guide");
const binary = path.resolve(
  baseDir,
  process.platform === "win32" ? "windforce-core.exe" : "windforce-core",
);

let server = null;
let receiver = null;
let receiverUrl = "";

function stopServer() {
  if (server && server.exitCode === null) server.kill();
  server = null;
  if (receiver) receiver.close();
  receiver = null;
  receiverUrl = "";
}

export default {
  name: "windforce-core",
  baseUrl: envValue("WINDFORCE_CORE_UI_URL", `http://127.0.0.1:${port}/ui/`),
  apiBaseUrl: envValue("WINDFORCE_CORE_API_URL", `http://127.0.0.1:${port}/api/w/default`),
  guidePath: "docs/user-guide/web-ui.md",
  scenariosDir: "docs/ui-scenarios",
  screenshotsDir: "docs/assets/ui",
  viewport: { width: 1440, height: 980 },
  // Extra Chromium flags for the CDP fallback browser, e.g.
  // CHROME_ARGS=--no-sandbox in rootless containers.
  chromeArgs: (process.env.CHROME_ARGS || "").split(/\s+/).filter(Boolean),

  // The guide runs against the embedded Web UI of a standalone build so the
  // screenshots show exactly what `go build` ships. Requires bun and go.
  async start({ exec, waitForHttp }) {
    if (external) {
      await waitForHttp(this.baseUrl);
      return;
    }
    await rm(baseDir, { recursive: true, force: true });
    await mkdir(baseDir, { recursive: true });
    await exec("make", ["web-embed"]);
    await exec("go", ["build", "-o", binary, "./cmd/windforce-core"]);
    receiver = createHttpServer((request, response) => {
      const signature = request.headers["x-windforce-signature"];
      const eventID = request.headers["x-windforce-event"];
      let body = "";
      request.on("data", (chunk) => { body += chunk; });
      request.on("end", () => {
        if (request.method !== "POST" || !signature || !eventID || !body) {
          response.writeHead(400).end();
          return;
        }
        response.writeHead(204).end();
      });
    });
    await new Promise((resolve, reject) => {
      receiver.once("error", reject);
      receiver.listen(0, "127.0.0.1", resolve);
    });
    receiver.unref();
    const address = receiver.address();
    receiverUrl = `http://127.0.0.1:${address.port}/windforce/releases`;
    server = spawn(
      binary,
      [
        "standalone",
        "--dev",
        "--addr", `127.0.0.1:${port}`,
        "--store", path.join(baseDir, "store"),
        "--catalog", path.join(baseDir, "catalog.json"),
        "--state", path.join(baseDir, "state.json"),
        "--cache", path.join(baseDir, "cache"),
        "--webhook-allow-insecure-loopback",
        "--ui-host-url", "https://portal.example.test/console",
        "--ui-host-label", "Open host console",
        "--http-route-provider", "ui-guide-router",
      ],
      { cwd: baseDir, stdio: "ignore" },
    );
    server.unref();
    process.on("exit", stopServer);
    await waitForHttp(`http://127.0.0.1:${port}/readyz`, { timeoutMs: 30000 });
    if (!server || server.exitCode !== null) {
      throw new Error(`windforce-core standalone exited early; is port ${port} already in use?`);
    }
  },

  async seed({ api, exec }) {
    if (external) return;
    const workspaceResponse = await fetch(new URL("/api/workspaces", this.baseUrl), {
      method: "POST",
      headers: { "content-type": "application/json", "x-windforce-actor": "ui-guide@example.test" },
      body: JSON.stringify({ id: "operations", name: "Operations" }),
    });
    if (!workspaceResponse.ok) {
      throw new Error(`workspace seed failed: HTTP ${workspaceResponse.status} ${await workspaceResponse.text()}`);
    }
    await api("/git_sources/sample", {
      method: "POST",
      body: { app_key: "echo" },
    });
    await configureSampleExecutionLimits(exec);
    const sources = await api("/git_sources");
    const sample = sources.find((source) => source.name === "echo");
    let webhook = null;
    if (sample) {
      // A settings change so the Audit tab has a record to show.
      await api(`/git_sources/${sample.id}`, {
        method: "PATCH",
        headers: { "x-windforce-actor": "ui-guide@example.test" },
        body: { name: "echo-service" },
      });
      await api(`/git_sources/${sample.id}/sync`, {
        method: "POST",
        headers: { "x-windforce-actor": "ui-guide@example.test" },
      });
      webhook = await api("/webhooks", {
        method: "POST",
        headers: { "x-windforce-actor": "ui-guide@example.test" },
        body: {
          name: "Release notifications",
          endpoint: receiverUrl,
          event_types: [
            "windforce.release.published",
            "windforce.human_task.created",
          ],
          app_keys: ["echo"],
          enabled: true,
        },
      });
      await api(`/git_sources/${sample.id}/deploy`, {
        method: "POST",
        headers: { "x-windforce-actor": "ui-guide@example.test" },
        body: { confirm: true, message: "UI guide release" },
      });
      await waitForWebhookDelivery(api, webhook.subscription.id);
      await seedTriggers(api, this.baseUrl);
      await advanceSampleRepository(exec);
    }
    const clientToken = await api("/clients", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: { name: "Example Retailer" },
    });
    await api("/apps/echo/input-configs", {
      method: "PUT",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        action_key: "echo",
        client_id: clientToken.client.id,
        config: { message: "configured for Example Retailer", response_mode: "compact" },
        locked_keys: ["message"],
      },
    });
    await api("/variables", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        path: "deployment/region",
        value: "ap-northeast-2",
        is_secret: false,
        description: "Primary runtime region",
      },
    });
    await api("/variables", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        path: "credentials/partner-token",
        value: "ui-guide-secret",
        is_secret: true,
        description: "Partner API credential",
      },
    });
    await api("/resource-types", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        name: "http-connection",
        version: "1.0.0",
        description: "HTTP endpoint with a Secret Variable reference",
        schema: {
          type: "object",
          properties: {
            base_url: { type: "string" },
            token: { type: "string" },
          },
          required: ["base_url", "token"],
          additionalProperties: false,
        },
      },
    });
    await api("/resources", {
      method: "POST",
      headers: { "x-windforce-actor": "ui-guide@example.test" },
      body: {
        path: "partners/acme",
        resource_type: "http-connection@1.0.0",
        description: "Acme partner connection",
        value: {
          base_url: "https://api.example.test",
          token: "$var:credentials/partner-token",
        },
      },
    });
    await waitForClientConfigRun(clientToken.client.id, clientToken.api_token);
    // The standalone local store replaces its JSON file after the worker
    // publishes the terminal result. Let that final Windows file operation
    // settle before browser scenarios begin issuing concurrent reads.
    await sleep(500);
  },

  async stop() {
    stopServer();
  },
};

async function seedTriggers(api, baseUrl) {
  const actorHeaders = { "x-windforce-actor": "ui-guide@example.test" };
  const signingSecret = "ui-guide-trigger-secret";
  const inboundWebhook = await api("/triggers", {
    method: "POST",
    headers: actorHeaders,
    body: {
      name: "Partner events",
      kind: "webhook",
      app: "echo",
      action: "echo",
      completion: { mode: "poll" },
      response: { mode: "async" },
      config: {
        signature_header: "X-WF-Signature-256",
        delivery_id_header: "X-WF-Delivery-Id",
        correlation_header: "X-WF-Correlation-Id",
        input_mode: "json",
      },
      secret_config: { secret: signingSecret },
    },
  });
  await api("/triggers", {
    method: "POST",
    headers: actorHeaders,
    body: {
      name: "Morning check",
      kind: "schedule",
      app: "echo",
      action: "echo",
      completion: { mode: "poll" },
      config: {
        cron: "0 9 * * *",
        timezone: "Asia/Seoul",
        input: { message: "scheduled health check" },
      },
    },
  });
  await api("/triggers", {
    method: "POST",
    headers: actorHeaders,
    body: {
      name: "Order queue",
      kind: "rabbitmq",
      app: "echo",
      action: "echo",
      completion: { mode: "none" },
      config: {
        queue: "orders.windforce",
        prefetch: 8,
        concurrency: 4,
        delivery_id_header: "x-source-delivery-id",
        input_mode: "json",
      },
      secret_config: { url: "amqps://ui-guide:ui-guide@broker.example.test/vhost" },
    },
  });
  await api(`/triggers/${encodeURIComponent(inboundWebhook.id)}/enable`, {
    method: "POST",
    headers: actorHeaders,
  });
  const route = await api(`/triggers/${encodeURIComponent(inboundWebhook.id)}/routes`, {
    method: "POST",
    headers: actorHeaders,
    body: {
      hostname: "hooks.example.test",
      path: "/echo/partner-events",
      visibility: "public",
      provider: "ui-guide-router",
    },
  });
  await api(`/http-route-bindings/${encodeURIComponent(route.id)}/status`, {
    method: "PUT",
    headers: { "x-windforce-actor": "provider:ui-guide-router" },
    body: {
      state: "ready",
      public_url: "https://hooks.example.test/echo/partner-events",
      observed_generation: route.generation,
    },
  });

  const body = JSON.stringify({ message: "partner delivery" });
  const signature = createHmac("sha256", signingSecret).update(body).digest("hex");
  const response = await fetch(
    new URL(
      `/api/v1/workspaces/default/triggers/${encodeURIComponent(inboundWebhook.id)}/events`,
      baseUrl,
    ),
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-wf-signature-256": `sha256=${signature}`,
        "x-wf-delivery-id": "ui-guide-partner-delivery-1",
      },
      body,
    },
  );
  if (response.status !== 202) {
    throw new Error(`Trigger seed delivery failed: HTTP ${response.status} ${await response.text()}`);
  }
}

async function advanceSampleRepository(exec) {
  const sampleBase = path.join(baseDir, ".data", "sample-repos", "default", "echo");
  const worktree = path.join(sampleBase, "work");
  const remote = path.join(sampleBase, "remote.git");
  await exec("git", ["-C", worktree, "commit", "--allow-empty", "-m", "Prepare next sample release"]);
  await exec("git", ["-C", worktree, "push", remote, "HEAD:refs/heads/main"]);
}

async function configureSampleExecutionLimits(exec) {
  const sampleBase = path.join(baseDir, ".data", "sample-repos", "default", "echo");
  const worktree = path.join(sampleBase, "work");
  const remote = path.join(sampleBase, "remote.git");
  const manifestPath = path.join(worktree, "windforce.json");
  const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
  manifest.executionLimits = {
    concurrency: [
      { id: "message-group", maxConcurrent: 2, inputPointers: ["/message"] },
    ],
  };
  manifest.actions.echo.executionLimits = {
    concurrency: [
      { id: "echo-message", maxConcurrent: 1, inputPointers: ["/message"] },
    ],
  };
  await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
  await exec("git", ["-C", worktree, "add", "windforce.json"]);
  await exec("git", ["-C", worktree, "commit", "-m", "Add sample execution limits"]);
  await exec("git", ["-C", worktree, "push", remote, "HEAD:refs/heads/main"]);
}

async function waitForWebhookDelivery(api, subscriptionID) {
  for (let attempt = 0; attempt < 80; attempt += 1) {
    const page = await api(`/webhooks/${encodeURIComponent(subscriptionID)}/deliveries?limit=5`);
    const release = page.items.find((item) => item.event.type === "windforce.release.published");
    if (release?.delivery.state === "succeeded") return;
    if (release?.delivery.state === "failed") {
      throw new Error(`UI guide webhook delivery failed: ${release.delivery.error_summary || "unknown error"}`);
    }
    await sleep(100);
  }
  throw new Error("UI guide webhook delivery did not succeed");
}

async function waitForClientConfigRun(clientID, apiToken) {
  const runsURL = `http://127.0.0.1:${port}/api/v1/workspaces/default/runs`;
  const headers = { "authorization": `Bearer ${apiToken}`, "content-type": "application/json" };
  const rejected = await fetch(runsURL, {
    method: "POST",
    headers,
    body: JSON.stringify({ app: "echo", action: "echo", input: { message: "caller value" } }),
  });
  if (rejected.status !== 400) {
    throw new Error(`locked input was not rejected: HTTP ${rejected.status}`);
  }

  const admitted = await fetch(runsURL, {
    method: "POST",
    headers,
    body: JSON.stringify({ app: "echo", action: "echo", input: {} }),
  });
  if (!admitted.ok) {
    throw new Error(`client-config run admission failed: HTTP ${admitted.status} ${await admitted.text()}`);
  }
  const run = await admitted.json();

  for (let attempt = 0; attempt < 60; attempt += 1) {
    const response = await fetch(`${runsURL}/${encodeURIComponent(run.run_id)}/result`, { headers });
    const result = await response.json();
    if (response.status === 202) {
      await sleep(250);
      continue;
    }
    if (!response.ok) {
      throw new Error(`client-config run failed: HTTP ${response.status} ${JSON.stringify(result)}`);
    }
    const output = result.output ?? result.result ?? result;
    if (output?.input?.message !== "configured for Example Retailer") {
      throw new Error(`worker did not apply client input settings: ${JSON.stringify(result)}`);
    }
    if (result.client_id && result.client_id !== clientID) {
      throw new Error(`invocation run used the wrong client: ${JSON.stringify(result)}`);
    }
    return;
  }
  throw new Error("client-config run did not finish");
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
