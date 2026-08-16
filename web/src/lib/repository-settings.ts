import { translate } from "../shared/i18n";
import { ApiError, errorMessage, type GitSource, type ProbeResult } from "./api";
import { defaultGitCredentialPath } from "./git-credential";

export function repositoryLocationLocked(source: GitSource): boolean {
  return Boolean(source.last_synced_commit);
}

export function repositoryAccessLabel(source: GitSource): string {
  return source.creds_ref
    ? translate("repository.credentialConfigured")
    : translate("repository.public");
}

export function reconnectCredentialPath(source: GitSource): string {
  return source.creds_ref || defaultGitCredentialPath(source.name);
}

export function probePassed(probe: ProbeResult | null): boolean {
  return Boolean(probe?.reachable && probe.branch_exists);
}

export function probeErrorMessage(probe: ProbeResult): string {
  if (probe.code === "git_source_repository_unreachable") {
    return translate("repository.unreachableWithCredential");
  }
  return probe.error || translate("repository.unreachable");
}

export function sourceErrorMessage(cause: unknown): string {
  if (!(cause instanceof ApiError)) return errorMessage(cause);
  switch (cause.code) {
    case "server.git_source_repository_unreachable":
      return translate("repository.unreachableWithCredential");
    case "server.git_source_branch_not_found":
      return translate("repository.branchNotFoundDetailed");
    case "server.git_source_subpath_invalid":
      return translate("repository.invalidSubpath");
    case "server.git_source_credential_unavailable":
      return translate("repository.credentialUnavailable");
    case "server.git_source_placement_requires_sync":
      return translate("repository.placementAfterSync");
    case "server.git_source_contract_invalid":
      return translate("repository.contractInvalid", { detail: cause.detail });
    default:
      return errorMessage(cause);
  }
}
