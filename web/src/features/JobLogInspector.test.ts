import { describe, expect, test } from "vitest";
import { shortOpaqueDigest } from "./JobLogInspector";

describe("shortOpaqueDigest", () => {
  test("keeps the algorithm and enough digest material to compare pins", () => {
    expect(shortOpaqueDigest("hmac-sha256:0123456789abcdef")).toBe("hmac-sha256:0123456789ab");
    expect(shortOpaqueDigest("sha256:fedcba9876543210")).toBe("sha256:fedcba987654");
  });

  test("shortens values without an algorithm prefix", () => {
    expect(shortOpaqueDigest("0123456789abcdef", 8)).toBe("01234567");
  });
});
