export default {
  id: "host-console-navigation-mobile",
  title: "Return to the host console on mobile",
  description:
    "The configured return action remains in the top bar as a compact icon with the same accessible label.",
  screenshot: "docs/assets/ui/host-console-navigation-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the embedded Web UI on a narrow screen.",
    "Use the arrow icon in the top bar to return to the configured host console.",
    "The icon keeps the full host-console label for assistive technology and pointer tooltips.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.waitForSelector("a[aria-label='Back to host console']");
    await capture();
  },
};
