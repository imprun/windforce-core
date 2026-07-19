export default {
  order: 8.5,
  id: "provisioning",
  title: "Import and export provisioning state",
  description:
    "Provisioning exports a redacted workspace snapshot and imports repeatable app, credential, client, input-setting, and webhook resources through dry-run first.",
  screenshot: "docs/assets/ui/provisioning.png",
  guide: [
    "Open Settings from the sidebar and choose Provisioning.",
    "Export the current workspace as YAML or JSON for review.",
    "Paste or load a provisioning document, run Dry-run, then Apply only after validation succeeds.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.clickText("Settings");
    await page.clickText("Provisioning");
    await page.waitForSelector(".provisioningEditor");
    await page.clickText("Export");
    await page.clickText("Export snapshot");
    await page.waitForSelector(".provisioningCode");
    await capture(this.id);
  },
};
