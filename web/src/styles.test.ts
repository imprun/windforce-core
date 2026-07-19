import { describe, expect, test } from "bun:test";

const styles = await Bun.file(new URL("./styles.css", import.meta.url)).text();

describe("table column alignment", () => {
  test("does not override every non-first table header", () => {
    expect(styles).not.toContain(".table th:not(:first-child)");
  });

  test("uses the shared numeric cell alignment contract", () => {
    expect(styles).toMatch(/\.numCell\s*\{[^}]*text-align:\s*right;/s);
    expect(styles).toMatch(/\.table th\.numCell\s*\{[^}]*text-align:\s*right;/s);
  });
});

describe("provisioning layout", () => {
  test("keeps commands next to the active provisioning document", () => {
    expect(styles).toMatch(/\.provisioningWorkspace\s*\{[^}]*grid-template-columns:\s*minmax\(520px,\s*1fr\)\s*390px;/s);
    expect(styles).toMatch(/\.provisioningSidePanel\s*\{[^}]*position:\s*sticky;/s);
    expect(styles).toMatch(/\.provisioningEditor\s*\{[^}]*min-height:\s*560px;/s);
    expect(styles).toMatch(/\.provisioningCode\s*\{[^}]*max-height:\s*70vh;/s);
  });
});
