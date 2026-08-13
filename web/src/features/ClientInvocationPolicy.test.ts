import { describe, expect, test } from "vitest";
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
});
