/** @vitest-environment jsdom */

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  useAsync: vi.fn(),
  reload: vi.fn(),
}));

vi.mock("../lib/app-context", () => ({
  useApp: () => ({ api: {}, notify: vi.fn() }),
  useAsync: mocks.useAsync,
}));

import { AppRuntimeConfiguration } from "./AppRuntimeConfiguration";

describe("AppRuntimeConfiguration", () => {
  afterEach(cleanup);

  beforeEach(() => {
    mocks.useAsync.mockReset();
    mocks.reload.mockReset();
  });

  test("makes lifecycle state and App-owned revisions visible", () => {
    mocks.useAsync.mockReturnValue({
      data: runtimeData("active"),
      loading: false,
      error: null,
      reload: mocks.reload,
    });

    render(<AppRuntimeConfiguration appKey="orders" />);

    expect(screen.getByText("Active")).toBeTruthy();
    expect(screen.getByText("credentials/token")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Start retirement/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Emergency revoke/i })).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: /New Variable/i }) as HTMLButtonElement).disabled,
    ).toBe(false);
  });

  test("prevents new configuration edits while the App is retiring", () => {
    mocks.useAsync.mockReturnValue({
      data: runtimeData("tombstoned"),
      loading: false,
      error: null,
      reload: mocks.reload,
    });

    render(<AppRuntimeConfiguration appKey="orders" />);

    expect(screen.getByText("Retiring")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Reactivate/i })).toBeTruthy();
    expect(
      (screen.getByRole("button", { name: /New Variable/i }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: /Purge configuration/i }) as HTMLButtonElement).disabled,
    ).toBe(false);
  });
});

function runtimeData(state: "active" | "tombstoned") {
  return {
    variables: [
      {
        owner_scope: "app" as const,
        app_key: "orders",
        path: "credentials/token",
        value: "",
        is_secret: true,
        description: "Partner token",
        revision: 7,
        updated_at: "2026-08-16T00:00:00Z",
      },
    ],
    resources: [],
    resourceTypes: [],
    lifecycle: {
      workspaceId: "default",
      appKey: "orders",
      state,
      reason: state === "tombstoned" ? "retiring" : "",
      actor: "operator",
      revision: 2,
      updatedAt: "2026-08-16T00:00:00Z",
    },
    audit: [],
  };
}
