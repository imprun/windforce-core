import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const source = await readFile(new URL("./ClientDetailPage.tsx", import.meta.url), "utf8");

describe("app-caller detail", () => {
  test("focuses on identity, app access, credentials, and audit", () => {
    expect(source).toContain('{ key: "overview", label: "clients.tab.overview" }');
    expect(source).toContain('{ key: "audit", label: "clients.tab.audit" }');
    expect(source).toContain("<ClientInvocationPolicy");
    expect(source).not.toContain("ClientInputSettings");
    expect(source).not.toContain("clientInputConfigs");
    expect(source).not.toContain("clients.configurationSummary");
  });
});
