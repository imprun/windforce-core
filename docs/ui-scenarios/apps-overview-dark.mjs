export default {
  order: 2,
  id: "apps-overview-dark",
  title: "Review the execution workspace in dark mode",
  description:
    "Dark mode keeps the green runtime identity while separating the shell, work surface, selection, and status colors by luminance.",
  screenshot: "docs/assets/ui/apps-overview-dark.png",
  guide: [
    "Switch the execution workspace to dark mode from the appearance control.",
    "Confirm the near-black green shell stays distinct from the dark work surface.",
    "Use the mint selection and focus colors to identify the active workspace and navigation item.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.evaluate(() => {
      document.documentElement.setAttribute("data-theme", "dark");
    });
    await capture(this.id);
  },
};
