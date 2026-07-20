export default {
  order: 3.5,
  id: "sync-source",
  title: "Synchronize source",
  description:
    "Sync source fetches the tracked branch, validates the source contract, and stores the exact revision without changing the active release.",
  screenshot: "docs/assets/ui/sync-source.png",
  guide: [
    "Open an app and find Sync source next to Publish Release.",
    "Click Sync source.",
    "Confirm that Sync source changes to Source current and Publish Release becomes available when the commit changed.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.waitForFunction(() => {
      const button = document.querySelector("#syncSourceButton");
      return button instanceof HTMLButtonElement && !button.disabled;
    });
    await page.click("#syncSourceButton");
    await page.waitForFunction(() => {
      return (
        document.querySelector("#syncSourceButton")?.getAttribute("data-checked") === "true" ||
        document.querySelector("#toast")?.textContent
      );
    });
    const result = await page.evaluate(() => ({
      checked: document.querySelector("#syncSourceButton")?.getAttribute("data-checked"),
      message: document.querySelector("#toast")?.textContent?.trim(),
    }));
    if (result.checked !== "true") {
      if (!result.message?.startsWith("Synchronized ")) {
        throw new Error(`source synchronization failed: ${result.message || "unknown error"}`);
      }
      await page.waitForSelector('#syncSourceButton[data-checked="true"]');
    }
    await capture(this.id);
  },
};
