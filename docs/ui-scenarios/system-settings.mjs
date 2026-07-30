export default {
  order: 9.5,
  id: "system-settings",
  title: "Inspect system configuration",
  description:
    "System settings presents control-plane readiness, APIs, backends, security configuration, and runtime values without browser-local credential duplication.",
  screenshot: "docs/assets/ui/system-settings.png",
  guide: [
    "Open Settings from the sidebar and choose System.",
    "Check control-plane readiness and the active service and workspace.",
    "Review enabled APIs, backend integrations, and security configuration.",
    "Use runtime configuration for diagnostics; secret values are never shown.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/system']");
    await page.waitForText("main", "APIs and interfaces");
    await page.waitForText("main", "Runtime configuration");
    await capture(this.id);
  },
};
