export default {
  order: 3.7,
  id: "app-runtime-configuration",
  title: "Manage App-owned runtime configuration",
  description:
    "The App detail keeps exact App-owned Variables and Resources together with graceful retirement, emergency revoke, audit, and purge controls.",
  screenshot: "docs/assets/ui/app-runtime-configuration.png",
  guide: [
    "Open a released App and choose App settings, then Runtime configuration.",
    "Review the active lifecycle state and revision before changing access.",
    "Manage only the Variables and typed Resources owned by this exact App scope.",
    "Use retirement before purge; reserve emergency revoke and force purge for incident response.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.click("a[href$='/settings/runtime-config']");
    await page.waitForSelector(".appRuntimeConfiguration .runtimeConfigTable tbody tr");
    await page.click('[data-ui-guide="app-runtime-retire"]');
    await page.waitForSelector("[role=dialog] textarea");
    await page.fill("[role=dialog] textarea", "UI guide graceful retirement acceptance");
    await page.click('[data-ui-guide="app-runtime-lifecycle-save"]');
    await page.waitForSelector('[data-ui-guide="app-runtime-reactivate"]');
    await page.click('[data-ui-guide="app-runtime-reactivate"]');
    await page.waitForSelector("[role=dialog] textarea");
    await page.fill("[role=dialog] textarea", "UI guide reactivation acceptance");
    await page.click('[data-ui-guide="app-runtime-lifecycle-save"]');
    await page.waitForSelector('[data-ui-guide="app-runtime-retire"]');
    await page.click('[data-ui-guide="app-runtime-tab-resources"]');
    await page.waitForSelector(".runtimeConfigTable tbody tr");
    await page.click('[data-ui-guide="app-runtime-tab-variables"]');
    await capture(this.id);
  },
};
