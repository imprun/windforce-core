export default {
  id: "host-console-navigation-mobile",
  title: "Open host management on mobile",
  description:
    "The configured return action remains available as a compact control in the mobile top bar.",
  screenshot: "docs/assets/ui/host-console-navigation-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the embedded Web UI on a narrow screen.",
    "Use the labelled host action in the top bar to open the configured management console.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("header [data-testid='host-console-action']");
    await capture();
  },
};
