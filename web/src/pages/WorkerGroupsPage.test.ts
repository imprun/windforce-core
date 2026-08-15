import { describe, expect, test } from "vitest";
import type { WorkerGroupInventoryItem } from "../lib/api";
import { summarizeWorkerGroups } from "./WorkerGroupsPage";

function group(name: string, values: Partial<WorkerGroupInventoryItem>): WorkerGroupInventoryItem {
  return {
    group: name,
    status: "ready",
    workspace_allowed: true,
    managed: true,
    active_credentials: 1,
    run_state: "running",
    run_state_revision: 0,
    live_workers: 1,
    unmanaged_live_workers: 0,
    available_slots: 2,
    active_leases: 0,
    running_jobs: 0,
    quiescent: false,
    tags: [],
    labels: [],
    execution_profiles: [],
    engine_versions: [],
    build_revisions: [],
    version_or_build_drift: false,
    ...values,
  };
}

describe("WorkerGroupsPage", () => {
  test("keeps admin-only unauthorized groups out of workspace capacity totals", () => {
    const result = summarizeWorkerGroups([
      group("ready", {}),
      group("draining", { status: "draining", available_slots: 0, live_workers: 3 }),
      group("drift", { status: "degraded", version_or_build_drift: true }),
      group("hidden", { workspace_allowed: false, live_workers: 20, available_slots: 40 }),
    ]);

    expect(result).toEqual({
      usableGroups: 3,
      liveWorkers: 5,
      availableSlots: 4,
      attentionGroups: 2,
    });
  });
});
