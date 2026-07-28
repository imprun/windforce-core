export default {
  order: 2.5,
  id: "korean-console",
  title: "Use the console in Korean",
  description:
    "The embedded console supports Korean across navigation and product screens while preserving API identifiers and user data.",
  screenshot: "docs/assets/ui/korean-console.png",
  guide: [
    "Open the language menu in the top bar and choose 한국어.",
    "Confirm navigation, page headings, status labels, validation, and errors use Korean.",
    "Reload the page and confirm the selected language remains active.",
    "Keep API paths, app and Action keys, event types, logs, and user-entered values unchanged.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector('[title="Language"]');
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.waitForText("main", "앱");
    const localeState = await page.evaluate(() => ({
      lang: document.documentElement.lang,
      stored: localStorage.getItem("wf.locale"),
    }));
    if (localeState.lang !== "ko" || localeState.stored !== "ko") {
      throw new Error(`Korean locale was not persisted: ${JSON.stringify(localeState)}`);
    }
    await page.goto();
    await page.waitForText("main", "앱");
    await capture(this.id);
    await page.waitForSelector('[title="언어"]');
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
  },
};
