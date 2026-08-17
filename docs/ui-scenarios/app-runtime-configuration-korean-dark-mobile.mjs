export default {
  order: 3.71,
  id: "app-runtime-configuration-korean-dark-mobile",
  title: "Review App runtime configuration in Korean dark mobile mode",
  description:
    "Lifecycle state, App-owned values, and destructive controls remain readable in Korean on a narrow dark surface.",
  screenshot: "docs/assets/ui/app-runtime-configuration-korean-dark-mobile.png",
  viewport: { width: 390, height: 844 },
  guide: [
    "Switch the console to Korean and open 앱 설정, then 실행 구성.",
    "Review lifecycle state and exact App-owned values on a narrow screen.",
    "Confirm retirement, emergency revoke, and purge remain distinguishable without relying on color alone.",
  ],
  async run({ page, capture }) {
    await page.goto();
    await page.evaluate(() => localStorage.setItem("wf.locale", "ko"));
    await page.goto();
    await page.waitForSelector("#appList .tableRow");
    await page.click("#appList .cellTitle");
    await page.click('[data-ui-guide="app-settings"]');
    await page.click("a[href$='/settings/runtime-config']");
    await page.waitForSelector(".appRuntimeConfiguration .runtimeConfigTable tbody tr");
    await page.evaluate(() => document.documentElement.setAttribute("data-theme", "dark"));
    await capture(this.id);
  },
};
