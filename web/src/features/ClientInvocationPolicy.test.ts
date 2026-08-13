import { describe, expect, test } from "vitest";
import { setLocale, translate } from "../shared/i18n";
import { normalizeAllowedTargets } from "./ClientInvocationPolicy";

describe("client invocation policy", () => {
  test("normalizes exact app and app/action targets before submission", () => {
    expect(normalizeAllowedTargets("orders/create\n billing,orders/create\norders")).toEqual([
      "billing",
      "orders",
      "orders/create",
    ]);
  });

  test("keeps an empty restricted target list as deny all", () => {
    expect(normalizeAllowedTargets(" \n, ")).toEqual([]);
  });

  test("uses customer-facing app access language in English and Korean", async () => {
    await setLocale("en");
    expect(translate("clients.policy.title")).toBe("App access");
    expect(translate("clients.policy.modeAll")).toBe("All apps and actions");
    expect(translate("clients.policy.modeRestricted")).toBe("Selected apps and actions");
    expect(translate("clients.invocationToken")).toBe("API credential");

    await setLocale("ko");
    try {
      expect(translate("navigation.clientRegistry")).toBe("고객");
      expect(translate("clients.policy.title")).toBe("앱 이용 권한");
      expect(translate("clients.policy.modeAll")).toBe("모든 앱과 액션");
      expect(translate("clients.policy.modeRestricted")).toBe("선택한 앱과 액션");
      expect(translate("clients.invocationToken")).toBe("API 자격 증명");
    } finally {
      await setLocale("en");
    }
  });
});
