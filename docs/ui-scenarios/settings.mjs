export default {
  order: 9,
  id: "settings",
  title: "Manage the active workspace",
  description:
    "Workspace settings keep identity, API access, Core connection details, and lifecycle controls in one place.",
  screenshot: "docs/assets/ui/settings.png",
  guide: [
    "Open Settings from the sidebar and choose Workspace.",
    "Update the active workspace identity without leaving its settings page.",
    "Create and store a named workspace token for an operator or trusted administrative tool.",
    "Copy the Core URL or workspace API base into a direct Core integration.",
    "Use Client Registry instead when an external app caller only needs the Invocation API.",
    "In hosted mode, manage account access and API credentials in the host console.",
    "Archive the workspace only from the lifecycle section at the end of the page.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/workspace']");
    await page.waitForText("main", "Workspace identity");
    await page.waitForSelector(".workspaceTokenCreate");
    await page.waitForText("main", "Core API connection");
    await page.waitForText("main", "Core URL");
    await page.waitForText("main", "Workspace API base");
    await capture(this.id);
  },
};
