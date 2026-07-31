import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const source = await readFile(new URL("./RuntimeConfigurationPage.tsx", import.meta.url), "utf8");

describe("runtime configuration", () => {
  test("uses shared accessible overlays and the design-system SelectControl", () => {
    expect(source).toContain("<Modal");
    expect(source).toContain("<ConfirmDialog");
    expect(source).toContain("<SelectControl");
    expect(source).not.toContain("window.confirm");
    expect(source).not.toContain("<select");
  });

  test("treats stored secrets as replacement-only values", () => {
    expect(source).toContain('item?.is_secret ? ""');
    expect(source).toContain("runtimeConfig.secretReplaceNotice");
    expect(source).not.toContain("getVariable(");
  });
});
