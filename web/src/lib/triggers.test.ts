import { describe, expect, test } from "vitest";
import type { TriggerDefinition } from "./api";
import {
  buildTriggerPayload,
  draftFromTrigger,
  emptyTriggerDraft,
  httpRouteProvider,
  triggerConfigSummary,
} from "./triggers";

function triggerFixture(overrides: Partial<TriggerDefinition> = {}): TriggerDefinition {
  return {
    id: "trg_1",
    workspace_id: "default",
    name: "Trigger",
    kind: "webhook",
    enabled: false,
    app: "orders",
    action: "ingest",
    config: {},
    has_secret: false,
    created_by: "operator",
    updated_by: "operator",
    created_at: "2026-07-28T00:00:00Z",
    updated_at: "2026-07-28T00:00:00Z",
    ...overrides,
  };
}

describe("trigger form payloads", () => {
  test("creates webhook triggers disabled with write-only secret config", () => {
    const draft = {
      ...emptyTriggerDraft(),
      name: "Partner orders",
      action: "ingest",
      webhookSecret: "secret-value",
    };

    expect(buildTriggerPayload(draft, "orders")).toEqual({
      payload: {
        name: "Partner orders",
        kind: "webhook",
        enabled: false,
        app: "orders",
        action: "ingest",
        config: {
          signature_header: "X-WF-Signature-256",
          delivery_id_header: "X-WF-Delivery-Id",
          correlation_header: "X-WF-Correlation-Id",
          input_mode: "json",
        },
        secret_config: { secret: "secret-value" },
      },
    });
  });

  test("preserves an existing secret when editing", () => {
    const trigger = triggerFixture({
      name: "Partner orders",
      enabled: true,
      config: { input_mode: "raw" },
      has_secret: true,
    });

    const result = buildTriggerPayload(draftFromTrigger(trigger), "orders", trigger);

    expect(result.error).toBeUndefined();
    expect(result.payload?.enabled).toBe(true);
    expect(result.payload?.secret_config).toBeUndefined();
    expect(result.payload?.config.input_mode).toBe("raw");
  });

  test("parses schedule input and validates malformed JSON", () => {
    const draft = {
      ...emptyTriggerDraft(),
      kind: "schedule" as const,
      name: "Daily reconciliation",
      action: "reconcile",
      scheduleInput: '{"scope":"daily"}',
    };

    expect(buildTriggerPayload(draft, "orders").payload?.config).toEqual({
      cron: "0 9 * * *",
      timezone: "Asia/Seoul",
      input: { scope: "daily" },
    });
    expect(buildTriggerPayload({ ...draft, scheduleInput: "{" }, "orders").error).toBe(
      "Schedule input must be valid JSON.",
    );
  });

  test("enforces RabbitMQ prefetch and connection URL", () => {
    const draft = {
      ...emptyTriggerDraft(),
      kind: "rabbitmq" as const,
      name: "Order queue",
      action: "ingest",
      queue: "orders.windforce",
      rabbitMQURL: "amqps://broker.example.test/vhost",
      concurrency: "4",
      prefetch: "2",
    };

    expect(buildTriggerPayload(draft, "orders").error).toContain("Prefetch");
    expect(
      buildTriggerPayload({ ...draft, prefetch: "8" }, "orders").payload?.secret_config,
    ).toEqual({ url: "amqps://broker.example.test/vhost" });
  });

  test("summarizes configs without secret material", () => {
    const trigger = triggerFixture({
      kind: "rabbitmq",
      config: { queue: "orders.windforce" },
    });
    expect(triggerConfigSummary(trigger)).toBe("orders.windforce");
  });

  test("shows public route controls only for an advertised Router Provider", () => {
    expect(
      httpRouteProvider({
        service: "windforce-core",
        workspace: "default",
        ready: true,
        planes: { http_routes: true },
        backends: { http_route_provider: true },
        auth: {},
        runtime_config: { http_route_provider: " kubernetes-gateway-api " },
      }),
    ).toBe("kubernetes-gateway-api");
    expect(
      httpRouteProvider({
        service: "windforce-core",
        workspace: "default",
        ready: true,
        planes: { http_routes: true },
        backends: { http_route_provider: false },
        auth: {},
        runtime_config: { http_route_provider: "stale-provider" },
      }),
    ).toBe("");
  });
});
