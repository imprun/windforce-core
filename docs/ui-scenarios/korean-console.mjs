export default {
  order: 2.5,
  id: "korean-console",
  title: "Use the console in Korean",
  description:
    "The embedded console supports Korean across navigation and product screens while preserving API identifiers and user data.",
  screenshot: "docs/assets/ui/korean-console.png",
  guide: [
    "Confirm the globe action displays the current language: EN in English and 한국어 in Korean.",
    "Select the globe action, then confirm navigation, page headings, status labels, validation, and errors use Korean.",
    "Reload the page and confirm the selected language remains active.",
    "Keep API paths, app and Action keys, event types, logs, and user-entered values unchanged.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "en"));
    await page.goto();
    await page.waitForSelector('button[aria-label="Switch to 한국어"]');
    const englishSwitcherLabel = await page.evaluate(
      () =>
        document.querySelector('button[aria-label="Switch to 한국어"]')?.textContent?.trim() ??
        "",
    );
    if (englishSwitcherLabel !== "EN") {
      throw new Error(`English locale switcher displayed ${JSON.stringify(englishSwitcherLabel)}`);
    }
    await page.click('button[aria-label="Switch to 한국어"]');
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
    const koreanSwitcherLabel = await page.evaluate(
      () =>
        document.querySelector('button[aria-label="English로 전환"]')?.textContent?.trim() ?? "",
    );
    if (koreanSwitcherLabel !== "한국어") {
      throw new Error(`Korean locale switcher displayed ${JSON.stringify(koreanSwitcherLabel)}`);
    }
    await capture(this.id);
    await page.waitForSelector('button[aria-label="English로 전환"]');
    await page.click('button[aria-label="English로 전환"]');
    await page.waitForText("main", "Apps");
  },
};
