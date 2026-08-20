/** @vitest-environment jsdom */

import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import type { WorkerGroupInventory } from "../lib/api";

const mocks = vi.hoisted(() => ({
  useAsync: vi.fn(),
  inventoryReload: vi.fn(),
  demandReload: vi.fn(),
}));

vi.mock("../lib/app-context", () => ({
  useApp: () => ({
    api: {},
    runtimeConfig: { workerGroupOperator: "core" },
  }),
  useAsync: mocks.useAsync,
}));

vi.mock("../components/Layout", () => ({
  Layout: ({ children }: { children: ReactNode }) => <main>{children}</main>,
}));

import { WorkerGroupsPage } from "./WorkerGroupsPage";

const inventory: WorkerGroupInventory = {
  workspace: "workspace-a",
  observed_at: "2026-08-15T12:00:00Z",
  groups: [
    {
      group: "group-ready",
      status: "ready",
      workspace_allowed: true,
      managed: true,
      active_credentials: 1,
      run_state: "running",
      run_state_revision: 0,
      live_workers: 1,
      pressure_accepting_workers: 1,
      pressure_paused_workers: 0,
      stale_pressure_workers: 0,
      pressure_reason_codes: [],
      unmanaged_live_workers: 0,
      total_slots: 2,
      occupied_slots: 1,
      available_slots: 1,
      active_leases: 1,
      running_jobs: 1,
      quiescent: false,
      tags: ["ready"],
      labels: [],
      execution_profiles: [],
      engine_versions: [],
      build_revisions: [],
      version_or_build_drift: false,
    },
  ],
};

describe("WorkerGroupsPage partial failures", () => {
  beforeEach(() => {
    mocks.useAsync.mockReset();
    mocks.inventoryReload.mockReset();
    mocks.demandReload.mockReset();
  });

  test("keeps inventory visible when execution demand fails", () => {
    mocks.useAsync
      .mockReturnValueOnce({
        data: inventory,
        loading: false,
        error: null,
        reload: mocks.inventoryReload,
      })
      .mockReturnValueOnce({
        data: null,
        loading: false,
        error: "execution demand unavailable",
        reload: mocks.demandReload,
      });

    render(<WorkerGroupsPage />);

    expect(screen.getByText("group-ready")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toContain("execution demand unavailable");
  });
});
