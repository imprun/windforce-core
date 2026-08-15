import { describe, expect, test } from "vitest";
import type { ExecutionDemand, PlacementCandidates, PlacementTargetCandidates } from "../lib/api";
import {
  actionReleaseLabels,
  executionDemandTargetsForAction,
  placementCapacity,
  placementTargetForAction,
  summarizeDemandTargets,
} from "./ExecutionPlacementPanel";

describe("execution placement", () => {
  test("shows the Action release-label default as the App and Action runsOn union", () => {
    expect(actionReleaseLabels(["linux", "browser"], ["gpu", "browser"])).toEqual([
      "browser",
      "gpu",
      "linux",
    ]);
  });

  test("selects the server-projected app and exact Action targets", () => {
    const placement = {
      workspace: "default",
      observed_at: "2026-08-15T00:00:00Z",
      targets: [
        { app: "echo", effective_tag: "default", action: undefined },
        { app: "echo", action: "run", effective_tag: "browser" },
      ],
    } as PlacementCandidates;

    expect(placementTargetForAction(placement, undefined)?.effective_tag).toBe("default");
    expect(placementTargetForAction(placement, "run")?.effective_tag).toBe("browser");
    expect(placementTargetForAction(placement, "missing")).toBeUndefined();
  });

  test("derives free and saturated slots only from eligible candidate groups", () => {
    const target = {
      matching_slots: 3,
      candidates: [
        { eligible: true, occupied_slots: 2, available_slots: 1 },
        { eligible: false, occupied_slots: 9, available_slots: 9 },
      ],
    } as PlacementTargetCandidates;

    expect(placementCapacity(target)).toEqual({
      totalSlots: 3,
      occupiedSlots: 2,
      availableSlots: 1,
      saturated: false,
    });

    target.candidates[0]!.occupied_slots = 3;
    target.candidates[0]!.available_slots = 0;
    expect(placementCapacity(target).saturated).toBe(true);
  });

  test("keeps old pinned targets separate while summarizing one Action backlog", () => {
    const demand = {
      targets: [
        {
          action: "run",
          effective_tag: "current",
          queued_jobs: 2,
          oldest_queued_at: "2026-08-15T11:55:00Z",
        },
        {
          action: "run",
          effective_tag: "legacy",
          queued_jobs: 1,
          oldest_queued_at: "2026-08-15T11:50:00Z",
        },
        { action: "other", effective_tag: "default", queued_jobs: 4 },
      ],
    } as ExecutionDemand;
    const targets = executionDemandTargetsForAction(demand, "run");

    expect(summarizeDemandTargets(targets)).toMatchObject({
      queuedJobs: 3,
      oldestQueuedAt: "2026-08-15T11:50:00Z",
      targets,
    });
    expect(targets.map((target) => target.effective_tag)).toEqual(["current", "legacy"]);
  });
});
