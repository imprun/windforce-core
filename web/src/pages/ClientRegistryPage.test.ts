import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const pageSource = await readFile(new URL("./ClientRegistryPage.tsx", import.meta.url), "utf8");

describe("client registry", () => {
  test("combines update time and actor into one compact last-change column", () => {
    expect(pageSource).toContain('translate("clients.lastChange")');
    expect(pageSource).toContain("<time");
    expect(pageSource).toContain("dateTime={client.updated_at}");
    expect(pageSource).toContain("{client.updated_by}</span>");
    expect(pageSource).not.toContain('translate("common.updatedBy")');
  });

  test("uses semantic badges for client token state", () => {
    expect(pageSource).toContain('"badge badge-good"');
    expect(pageSource).toContain('"badge badge-neutral"');
  });
});
