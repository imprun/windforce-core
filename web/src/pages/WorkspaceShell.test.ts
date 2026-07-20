import { describe, expect, test } from "bun:test";

const registrySource = await Bun.file(new URL("./WorkspacesPage.tsx", import.meta.url)).text();
const detailSource = await Bun.file(new URL("./WorkspaceDetailPage.tsx", import.meta.url)).text();

describe("workspace administration shell", () => {
  test("uses the instance shell for the registry", () => {
    expect(registrySource).toContain('scope="instance"');
  });

  test("uses the instance shell for detail, loading, and error states", () => {
    expect(detailSource.match(/scope="instance"/g)?.length).toBe(3);
  });

  test("aligns the workspace registry back control with the detail title", () => {
    expect(detailSource).toContain('className="button iconButton topbarTitleBack"');
    expect(detailSource.match(/titleLeading={backToWorkspaces}/g)?.length).toBe(3);
    expect(detailSource).not.toContain('actions={<Link className="button" to="/workspaces">');
  });
});
