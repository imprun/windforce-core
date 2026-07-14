import { describe, expect, test } from "bun:test";
import { forgeCommitURL, forgeName, forgeRawFileURL, forgeTreeURL } from "./repo";

describe("forgeTreeURL", () => {
  test("github https remotes", () => {
    expect(forgeTreeURL("https://github.com/org/repo.git", "abc123", "")).toBe(
      "https://github.com/org/repo/tree/abc123",
    );
    expect(forgeTreeURL("https://github.com/org/repo", "abc123", "apps/echo")).toBe(
      "https://github.com/org/repo/tree/abc123/apps/echo",
    );
  });

  test("gitlab https remotes use the /-/ layout", () => {
    expect(forgeTreeURL("https://gitlab.com/group/sub/repo.git", "abc123", "svc")).toBe(
      "https://gitlab.com/group/sub/repo/-/tree/abc123/svc",
    );
    expect(forgeTreeURL("https://gitlab.example.io/team/repo.git", "abc123", "")).toBe(
      "https://gitlab.example.io/team/repo/-/tree/abc123",
    );
  });

  test("ssh remotes convert to https", () => {
    expect(forgeTreeURL("git@github.com:org/repo.git", "abc123", "")).toBe(
      "https://github.com/org/repo/tree/abc123",
    );
  });

  test("unknown hosts and local paths return null", () => {
    expect(forgeTreeURL("/home/user/repo/remote.git", "abc123", "")).toBeNull();
    expect(forgeTreeURL("https://example.com/org/repo.git", "abc123", "")).toBeNull();
    expect(forgeTreeURL("https://github.com/org/repo.git", null, "")).toBeNull();
  });
});

describe("forgeCommitURL", () => {
  test("github and gitlab commit pages", () => {
    expect(forgeCommitURL("https://github.com/org/repo.git", "abc")).toBe(
      "https://github.com/org/repo/commit/abc",
    );
    expect(forgeCommitURL("https://gitlab.com/org/repo.git", "abc")).toBe(
      "https://gitlab.com/org/repo/-/commit/abc",
    );
  });
});

describe("forgeRawFileURL", () => {
  test("creates raw file URLs for supported forges", () => {
    expect(forgeRawFileURL("https://github.com/acme/widgets.git", "abc123", "docs/logo.png")).toBe(
      "https://github.com/acme/widgets/raw/abc123/docs/logo.png",
    );
    expect(forgeRawFileURL("https://gitlab.example.test/group/project.git", "abc123", "docs/logo.png")).toBe(
      "https://gitlab.example.test/group/project/-/raw/abc123/docs/logo.png",
    );
  });
});

describe("forgeName", () => {
  test("names the forge for link labels", () => {
    expect(forgeName("https://github.com/org/repo.git")).toBe("GitHub");
    expect(forgeName("git@gitlab.com:org/repo.git")).toBe("GitLab");
    expect(forgeName("/local/path.git")).toBeNull();
  });
});
