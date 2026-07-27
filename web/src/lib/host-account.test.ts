import { describe, expect, test } from "vitest";
import { parseHostAccount } from "./host-account";

describe("parseHostAccount", () => {
  test("accepts neutral account presentation and safe actions", () => {
    expect(
      parseHostAccount({
        label: "Jane Operator",
        detail: "Hosted account",
        account_url: "https://portal.example.test/account",
        account_label: "Manage account",
        logout_url: "/logout",
        logout_label: "Sign out",
      }),
    ).toMatchObject({
      label: "Jane Operator",
      detail: "Hosted account",
      accountURL: "https://portal.example.test/account",
      accountLabel: "Manage account",
      logoutLabel: "Sign out",
    });
  });

  test("rejects missing labels and executable actions", () => {
    expect(parseHostAccount({ detail: "Hosted account" })).toBeNull();
    expect(
      parseHostAccount({
        label: "Jane Operator",
        account_url: "javascript:alert(1)",
      }),
    ).toEqual({ label: "Jane Operator" });
  });
});
