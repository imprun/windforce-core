import { readFile } from "node:fs/promises";
import { describe, expect, test } from "vitest";

const styles = await readFile(new URL("./styles.css", import.meta.url), "utf8");
const designTokens = await readFile(new URL("./design-tokens.css", import.meta.url), "utf8");

describe("Windforce Web UI design contract", () => {
  test("loads the synchronized semantic token source before application styles", () => {
    expect(styles).toMatch(/^@import "tailwindcss";\r?\n@import "\.\/design-tokens\.css";/);
    expect(designTokens).toContain("Windforce Web UI semantic design tokens");
    expect(designTokens).toContain("--shell-sidebar-width: 16rem");
    expect(designTokens).toContain("--shell-header-height: 4.25rem");
    expect(designTokens).toContain("--content-max-width: 90rem");
    expect(designTokens).toContain("Pretendard");
    expect(designTokens).toContain("--background: #f4f7f5");
    expect(designTokens).toContain("--shell-active: #d7f56a");
    expect(designTokens).toContain("--background: #0d1713");
    expect(designTokens).not.toContain("#a9c7ff");
  });

  test("keeps literal palette values out of application styling", () => {
    expect(styles).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    expect(styles).not.toMatch(/\brgba?\(/i);
    expect(styles).not.toMatch(/\bhsla?\(/i);
    expect(styles).not.toContain('"Inter"');
  });

  test("maps legacy interaction classes to strong semantic tokens", () => {
    expect(styles).toMatch(
      /\.button\.primary\s*\{[^}]*background:\s*var\(--primary\);[^}]*border-color:\s*var\(--primary\);/s,
    );
    expect(styles).toMatch(
      /textarea:focus-visible,[^{]+\.segment:focus-visible\s*\{[^}]*outline:\s*2px solid var\(--ring\);/s,
    );
    expect(styles).not.toMatch(/\.button\.primary\s*\{[^}]*background:\s*var\(--accent\);/s);
  });
});

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
    expect(styles).toMatch(
      /\.provisioningWorkspace\s*\{[^}]*grid-template-columns:\s*minmax\(520px,\s*1fr\)\s*390px;/s,
    );
    expect(styles).toMatch(/\.provisioningSidePanel\s*\{[^}]*position:\s*sticky;/s);
    expect(styles).toMatch(/\.provisioningEditor\s*\{[^}]*min-height:\s*560px;/s);
    expect(styles).toMatch(/\.provisioningCode\s*\{[^}]*max-height:\s*70vh;/s);
  });

  test("uses the shared tab style for import and export modes", () => {
    expect(styles).toMatch(/\.tab\s*\{[^}]*border:\s*0;/s);
    expect(styles).toMatch(/\.provisioningModeTabs\s*\{[^}]*margin-bottom:\s*16px;/s);
    expect(styles).not.toMatch(/\.provisioningModeTabs button\.active\s*\{/);
  });
});

describe("workspace switcher layout", () => {
  test("keeps the workspace context compact in the topbar breadcrumb", () => {
    expect(styles).toMatch(
      /\.workspaceBreadcrumb \.workspaceSwitcherTrigger\s*\{[^}]*width:\s*auto;[^}]*max-width:/s,
    );
  });

  test("opens breadcrumb workspace popovers below their triggers", () => {
    expect(styles).toMatch(
      /\.workspaceBreadcrumb \.workspacePopover\s*\{[^}]*top:\s*calc\(100% \+ 0\.5rem\);[^}]*bottom:\s*auto;/s,
    );
  });

  test("pins hosted-console text to the semantic foreground", () => {
    expect(styles).toMatch(
      /a\[data-testid="host-console-action"\],[^{]+visited\s*\{[^}]*color:\s*var\(--foreground\);/s,
    );
  });
});
