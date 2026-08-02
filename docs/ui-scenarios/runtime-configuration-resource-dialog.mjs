export default {
  order: 9.62,
  id: "runtime-configuration-resource-dialog",
  title: "Compose a typed Resource",
  description:
    "The Resource editor keeps JSON, exact references, and Resource Type selection together without exposing Secret values.",
  screenshot: "docs/assets/ui/runtime-configuration-resource-dialog.png",
  guide: [
    "Open Variables & resources and choose Resources.",
    "Create a Resource and select its versioned Resource Type.",
    "Compose the JSON value with exact $var:path or $res:path strings.",
    "Save only after the Resource passes its registered JSON Schema.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.clickText("Settings");
    await page.click("a[href$='/settings/variables']");
    await page.clickText("Resources");
    await page.clickText("New Resource");
    await page.waitForSelector("[role='dialog'] .runtimeConfigEditor");
    await page.click("[role='dialog'] [role='combobox']");
    await page.waitForSelector("[role='listbox']");
    await capture(this.id);
  },
};
