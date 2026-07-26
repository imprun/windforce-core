export default {
  id: "host-console-navigation-mobile",
  title: "Open host management on mobile",
  description:
    "The configured return action stays with account context in the mobile drawer instead of competing with page context.",
  screenshot: "docs/assets/ui/host-console-navigation-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the embedded Web UI on a narrow screen.",
    "Open the mobile navigation drawer.",
    "Use the labelled host action above the account control to open the configured management console.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click('button[aria-label="Open navigation menu"]');
    await page.waitForSelector(".platformMobileNav [data-testid='host-console-action']");
    await capture();
  },
};
