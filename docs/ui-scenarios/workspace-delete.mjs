export default {
  order: 13,
  id: "workspace-delete",
  title: "Delete a local workspace",
  description:
    "Permanent deletion stays in the active workspace lifecycle section and requires the exact workspace name.",
  screenshot: "docs/assets/ui/workspace-delete.png",
  guide: [
    "Switch to the non-default workspace that should be removed.",
    "Open Settings, choose Workspace, and review the lifecycle section.",
    "Choose Delete workspace only when all runs, apps, triggers, configuration, credentials, and audit records can be removed.",
    "Type the workspace display name exactly; the permanent-delete action remains disabled until it matches.",
    "After deletion succeeds, the console switches to the protected default workspace.",
    "In hosted mode, use the host console instead of the Core-local lifecycle controls.",
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
    await page.waitForText("main", "Delete workspace");
    await page.clickText("Delete workspace");
    await page.waitForText("body", "Delete this workspace permanently?");
    await capture(this.id);
  },
};
