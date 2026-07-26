export default {
  order: 9.25,
  id: "mobile-navigation",
  title: "Navigate on a narrow screen",
  description:
    "The application shell keeps every primary engine destination available from an accessible mobile drawer.",
  screenshot: "docs/assets/ui/mobile-navigation.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Open the navigation menu in the top bar.",
    "Switch workspace, choose a product destination, or return to host management.",
    "Use the account control at the bottom for browser credentials.",
    "Close the drawer with its close control, the backdrop, or the Escape key.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.click('button[aria-label="Open navigation menu"]');
    await page.waitForSelector('nav[aria-label="Mobile"]');
    await capture(this.id);
  },
};
