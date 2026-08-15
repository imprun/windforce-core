import { describe, expect, test } from "vitest";
import type { PlacementCandidates } from "../lib/api";
import { actionReleaseLabels, placementTargetForAction } from "./ExecutionPlacementPanel";

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
});
