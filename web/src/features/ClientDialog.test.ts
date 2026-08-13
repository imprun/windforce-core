import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const dialogSource = await readFile(new URL("./ClientDialog.tsx", import.meta.url), "utf8");

describe("client dialog", () => {
  test("uses the shared confirmation dialog for irreversible client actions", () => {
    expect(dialogSource).toContain("<ConfirmDialog");
    expect(dialogSource).not.toContain("window.confirm");
    expect(dialogSource).toContain('setConfirmation("rotate")');
    expect(dialogSource).toContain('setConfirmation("revoke")');
    expect(dialogSource).toContain('setConfirmation("delete")');
  });

  test("reports successful token copy in the button without a toast", () => {
    expect(dialogSource).toContain("setTokenCopied(true)");
    expect(dialogSource).toContain('translate("common.copied")');
    expect(dialogSource).not.toContain('notify("ok", translate("clients.tokenCopied"))');
  });

  test("creates an app caller and initial app access in one request", () => {
    expect(dialogSource).toContain("invocation_policy:");
    expect(dialogSource).toContain('useState<"all" | "restricted">');
    expect(dialogSource).toContain('"restricted",');
    expect(dialogSource).not.toContain("updateClientInvocationPolicy(result.client.id");
  });
});
