import { describe, expect, test } from "vitest";
import { releaseActionState } from "./release-actions";

describe("releaseActionState", () => {
  test("requires synchronization before the first release", () => {
    expect(releaseActionState(undefined, undefined, false, false)).toEqual({
      syncLabel: "Sync source",
      syncDisabled: false,
      publishLabel: "Sync required",
      publishDisabled: true,
    });
  });

  test("enables publication when synchronized source differs from active release", () => {
    expect(releaseActionState("active", "latest", true, true)).toEqual({
      syncLabel: "Source current",
      syncDisabled: true,
      publishLabel: "Publish Release",
      publishDisabled: false,
    });
  });

  test("re-enables publication after rollback leaves a newer synchronized revision", () => {
    const rolledBackCommit = "stable-commit";
    const latestSynchronizedCommit = "newer-commit";
    expect(releaseActionState(rolledBackCommit, latestSynchronizedCommit, true, true)).toEqual({
      syncLabel: "Source current",
      syncDisabled: true,
      publishLabel: "Publish Release",
      publishDisabled: false,
    });
  });

  test("prevents an accidental duplicate release", () => {
    expect(releaseActionState("same", "same", true, true)).toEqual({
      syncLabel: "Source current",
      syncDisabled: true,
      publishLabel: "Up to date",
      publishDisabled: true,
    });
  });

  test("enables republishing when the active release has no execution bundle", () => {
    expect(releaseActionState("same", "same", true, false)).toEqual({
      syncLabel: "Source current",
      syncDisabled: true,
      publishLabel: "Republish required",
      publishDisabled: false,
    });
  });
});
