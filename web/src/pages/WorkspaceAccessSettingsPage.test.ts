import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const accessPageSource = await readFile(
  new URL("./WorkspaceAccessSettingsPage.tsx", import.meta.url),
  "utf8",
);

describe("workspace access settings", () => {
  test("keeps the token action aligned with its input in one control row", () => {
    expect(accessPageSource).toContain('className="workspaceTokenCreate"');
    expect(accessPageSource).toContain('className="fieldWithAction"');
    expect(accessPageSource).toContain('htmlFor="workspaceTokenName"');
    expect(accessPageSource).toContain('id="workspaceTokenName"');
    expect(accessPageSource).toContain('type="submit"');
  });
});
