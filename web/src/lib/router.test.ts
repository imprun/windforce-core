import { describe, expect, test } from "bun:test";
import { matchRoute } from "./router";

describe("matchRoute", () => {
  test("matches static routes", () => {
    expect(matchRoute("/jobs", "/jobs")).toEqual({});
    expect(matchRoute("/jobs", "/jobs/")).toEqual({});
    expect(matchRoute("/settings", "/jobs")).toBeNull();
  });

  test("captures params", () => {
    expect(matchRoute("/jobs/:id", "/jobs/abc-123")).toEqual({ id: "abc-123" });
    expect(matchRoute("/apps/:id/:tab?", "/apps/3/releases")).toEqual({ id: "3", tab: "releases" });
  });

  test("optional trailing params may be omitted", () => {
    expect(matchRoute("/apps/:id/:tab?", "/apps/3")).toEqual({ id: "3" });
  });

  test("rejects extra segments", () => {
    expect(matchRoute("/jobs/:id", "/jobs/a/b")).toBeNull();
    expect(matchRoute("/jobs", "/jobs/a")).toBeNull();
  });

  test("decodes encoded segments", () => {
    expect(matchRoute("/jobs/:id", "/jobs/a%2Fb")).toEqual({ id: "a/b" });
  });

  test("tolerates malformed percent escapes instead of throwing", () => {
    expect(matchRoute("/jobs/:id", "/jobs/abc%")).toEqual({ id: "abc%" });
  });
});
