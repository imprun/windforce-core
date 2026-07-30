export default {
  order: 9.1,
  id: "settings-mobile",
  title: "Connect standalone access on mobile",
  description:
    "Access settings keep credential creation, issued-token review, and CLI setup usable on a narrow screen.",
  screenshot: "docs/assets/ui/settings-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open Settings and choose Access.",
    "Issue a named workspace token without horizontal page overflow.",
    "Continue to the CLI and local-browser sections in the same responsibility-focused flow.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/access']");
    await page.waitForSelector(".workspaceTokenCreate");
    await page.waitForText("main", "Workspace API tokens");
    await capture(this.id);
  },
};
