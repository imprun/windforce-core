export default {
  order: 11,
  id: "workspace-detail",
  title: "Configure the active workspace",
  description:
    "Identity and lifecycle settings stay with the active workspace instead of creating a second registry detail context.",
  screenshot: "docs/assets/ui/workspace-detail.png",
  guide: [
    "Switch to the workspace from Manage workspaces.",
    "Open Settings and choose Workspace to change its display name, archive it, or permanently delete it.",
    "Choose Access to issue, rotate, or revoke named workspace credentials.",
    "Open Audit and select Workspace to review identity, credential, and lifecycle events.",
    "Return to Manage workspaces only when creating or switching workspace context.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.click("[data-testid='workspace-topbar-context'] .workspaceSwitcherTrigger");
    await page.evaluate(() => {
      const option = Array.from(document.querySelectorAll(".workspaceOption")).find((item) =>
        item.textContent.includes("operations"),
      );
      if (!option) throw new Error("operations workspace option not found");
      option.click();
    });
    await page.clickText("Settings");
    await page.click("a[href$='/settings/workspace']");
    await page.waitForText("main", "Workspace identity");
    await page.waitForText("main", "Workspace lifecycle");
    await page.waitForText("main", "Delete workspace");
    await page.evaluate(() => {
      const panel = Array.from(document.querySelectorAll(".panel")).find((item) =>
        item.textContent.includes("Workspace lifecycle"),
      );
      if (!panel) throw new Error("workspace lifecycle panel not found");
      panel.scrollIntoView({ block: "center" });
    });
    await capture(this.id);
  },
};
