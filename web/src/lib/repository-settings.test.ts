import { beforeEach, describe, expect, test } from "vitest";
import { setLocale } from "../shared/i18n";
import { ApiError, type GitSource } from "./api";
import {
  probeErrorMessage,
  probePassed,
  reconnectCredentialPath,
  repositoryAccessLabel,
  repositoryLocationLocked,
  sourceErrorMessage,
} from "./repository-settings";

beforeEach(() => setLocale("en"));

const source: GitSource = {
  id: 2,
  workspace_id: "default",
  name: "MLMWGM",
  repo_url: "https://example.test/gov24.git",
  branch: "main",
  subpath: "",
  creds_ref: "git/gov24/credential",
  kind: "external",
  created_at: "2026-07-13T00:00:00Z",
};

describe("repository settings policy", () => {
  test("locks repository location after the first synchronization", () => {
    expect(repositoryLocationLocked(source)).toBe(false);
    expect(repositoryLocationLocked({ ...source, last_synced_commit: "abc123" })).toBe(true);
  });

  test("does not expose credential paths in access labels", () => {
    expect(repositoryAccessLabel(source)).toBe("Credential configured");
    expect(repositoryAccessLabel({ ...source, creds_ref: "" })).toBe("Public repository");
  });

  test("keeps an existing credential path when rotating a credential", () => {
    expect(reconnectCredentialPath(source)).toBe("git/gov24/credential");
    expect(reconnectCredentialPath({ ...source, creds_ref: "", name: "Gov 24" })).toBe(
      "git/Gov-24/credential",
    );
  });

  test("requires both reachability and the selected branch", () => {
    expect(probePassed({ reachable: true, branch_exists: true })).toBe(true);
    expect(probePassed({ reachable: true, branch_exists: false })).toBe(false);
    expect(probePassed({ reachable: false, branch_exists: true })).toBe(false);
    expect(probePassed(null)).toBe(false);
  });

  test("explains private repository access failures without exposing Git output", () => {
    expect(
      probeErrorMessage({
        reachable: false,
        branches: [],
        code: "git_source_repository_unreachable",
        error: "repository cannot be reached with the provided Git credential",
      }),
    ).toContain("For a private repository");
    expect(
      sourceErrorMessage(
        new ApiError(
          "server.git_source_repository_unreachable",
          422,
          "fatal: Authentication failed for https://secret@example.test/repo.git",
        ),
      ),
    ).not.toContain("fatal");
  });

  test("shows the sanitized Sync manifest validation detail", () => {
    expect(
      sourceErrorMessage(
        new ApiError(
          "server.git_source_contract_invalid",
          422,
          'invalid app key "bad-app" in windforce.json',
        ),
      ),
    ).toContain('invalid app key "bad-app" in windforce.json');
  });
});
