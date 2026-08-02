export default {
  order: 9.6,
  id: "runtime-configuration",
  title: "Manage variables and resources",
  description:
    "Variables, write-only Secret Variables, typed Resources, and versioned Resource Types share one workspace-scoped console.",
  screenshot: "docs/assets/ui/runtime-configuration.png",
  guide: [
    "Open Settings and choose Variables & resources.",
    "Use Variables for scalar values and mark credentials as write-only Secrets.",
    "Use Resources for structured JSON that can compose exact $var:path and $res:path references.",
    "Register a versioned Resource Type when a Resource needs JSON Schema validation.",
    "Declare the required paths in each Action runtimeAccess block before publishing a release.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/variables']");
    await page.waitForSelector(".runtimeConfigTable tbody tr");
    await capture(this.id);
  },
};
