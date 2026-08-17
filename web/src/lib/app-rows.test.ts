import { describe, expect, test } from "vitest";
import type { AppSummary, GitSource } from "./api";
import { buildAppRows, findAppForSource } from "./app-rows";

describe("findAppForSource", () => {
  test("loads an active logical app from its re-registered source detail route", () => {
    expect(findAppForSource(source(2, "echo"), [app("echo", 1)])?.app_key).toBe("echo");
  });

  test("falls back to the source id for legacy registrations", () => {
    expect(findAppForSource(source(4), [app("legacy", 4)])?.app_key).toBe("legacy");
  });

  test("keeps a released App addressable after its repository source is removed", () => {
    expect(findAppForSource(null, [app("legacy", 4)], 4)?.app_key).toBe("legacy");
  });
});

describe("buildAppRows", () => {
  test("joins an active release to its re-registered logical source", () => {
    const rows = buildAppRows([source(2, "echo", "2026-08-02T00:00:00Z")], [app("echo", 1)]);

    expect(rows).toHaveLength(1);
    expect(rows[0]?.source?.id).toBe(2);
    expect(rows[0]?.app?.git_source_id).toBe(1);
  });

  test("uses the newest validated source when older and newer registrations coexist", () => {
    const rows = buildAppRows(
      [source(1, "echo", "2026-08-01T00:00:00Z"), source(2, "echo", "2026-08-02T00:00:00Z")],
      [app("echo", 1)],
    );

    expect(rows).toHaveLength(1);
    expect(rows[0]?.source?.id).toBe(2);
    expect(rows[0]?.app?.app_key).toBe("echo");
  });

  test("preserves pending sources, legacy source-id matches, and orphan releases", () => {
    const pending = source(3);
    const legacy = source(4);
    const rows = buildAppRows([pending, legacy], [app("legacy", 4), app("orphan", 9)]);

    expect(rows.map((row) => [row.source?.id ?? null, row.app?.app_key ?? null])).toEqual([
      [3, null],
      [4, "legacy"],
      [null, "orphan"],
    ]);
  });
});

function source(id: number, appKey?: string, lastSyncedAt?: string): GitSource {
  return {
    id,
    workspace_id: "default",
    name: `source-${id}`,
    app_key: appKey,
    repo_url: `https://example.test/source-${id}.git`,
    branch: "main",
    subpath: "",
    creds_ref: "",
    kind: "external",
    last_synced_at: lastSyncedAt,
    created_at: `2026-08-0${id}T00:00:00Z`,
  };
}

function app(appKey: string, sourceID: number): AppSummary {
  return {
    id: appKey,
    workspace_id: "default",
    app_key: appKey,
    git_source_id: sourceID,
    commit_sha: "0123456789abcdef",
    entrypoint: "main.ts",
    tag: "default",
    timeout_s: 60,
    script_lang: "typescript",
    bundle_status: "ready",
    updated_at: "2026-08-01T00:00:00Z",
    effective_route_tag: "default",
    actions_count: 1,
  };
}
