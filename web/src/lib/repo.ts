// Build forge (GitHub/GitLab) web URLs from a registered repo_url, pinned to
// a release commit. Returns null for hosts the UI cannot map (e.g. local
// paths in development); callers fall back to plain text.

type ForgeKind = "github" | "gitlab";

type ForgeBase = {
  base: string;
  kind: ForgeKind;
};

export function forgeBase(repoURL: string): ForgeBase | null {
  let url = (repoURL || "").trim();
  const ssh = url.match(/^(?:ssh:\/\/)?git@([^:/]+)[:/](.+)$/);
  if (ssh) url = `https://${ssh[1]}/${ssh[2]}`;
  if (!/^https?:\/\//i.test(url)) return null;
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return null;
  }
  const path = parsed.pathname.replace(/\.git$/, "").replace(/\/+$/, "");
  if (!path || path === "/") return null;
  const host = parsed.hostname.toLowerCase();
  const kind: ForgeKind | null =
    host === "github.com" || host.endsWith(".github.com")
      ? "github"
      : host === "gitlab.com" || host.split(".").some((part) => part.includes("gitlab"))
        ? "gitlab"
        : null;
  if (!kind) return null;
  return { base: `${parsed.origin}${path}`, kind };
}

export function forgeTreeURL(
  repoURL: string,
  commit: string | null | undefined,
  subpath?: string,
): string | null {
  const forge = forgeBase(repoURL);
  if (!forge || !commit) return null;
  const cleanSubpath = (subpath || "").replace(/^\/+|\/+$/g, "");
  const suffix = cleanSubpath ? `/${cleanSubpath}` : "";
  return forge.kind === "github"
    ? `${forge.base}/tree/${commit}${suffix}`
    : `${forge.base}/-/tree/${commit}${suffix}`;
}

export function forgeRawFileURL(
  repoURL: string,
  commit: string | null | undefined,
  path: string,
): string | null {
  const forge = forgeBase(repoURL);
  if (!forge || !commit) return null;
  const cleanPath = path.replace(/^\/+/, "");
  if (!cleanPath) return null;
  return forge.kind === "github"
    ? `${forge.base}/raw/${commit}/${cleanPath}`
    : `${forge.base}/-/raw/${commit}/${cleanPath}`;
}

export function forgeCommitURL(repoURL: string, commit: string | null | undefined): string | null {
  const forge = forgeBase(repoURL);
  if (!forge || !commit) return null;
  return forge.kind === "github"
    ? `${forge.base}/commit/${commit}`
    : `${forge.base}/-/commit/${commit}`;
}

export function forgeName(repoURL: string): string | null {
  const forge = forgeBase(repoURL);
  if (!forge) return null;
  return forge.kind === "github" ? "GitHub" : "GitLab";
}
