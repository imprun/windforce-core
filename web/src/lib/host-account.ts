export interface HostAccount {
  label: string;
  detail?: string;
  accountURL?: string;
  accountLabel?: string;
  logoutURL?: string;
  logoutLabel?: string;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function text(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim();
  return normalized || undefined;
}

function webURL(value: unknown): string | undefined {
  const raw = text(value);
  if (!raw) return undefined;
  try {
    const target = new URL(raw, globalThis.location?.origin || "https://runtime.invalid");
    if (!["http:", "https:"].includes(target.protocol) || !target.host) return undefined;
    return target.toString();
  } catch {
    return undefined;
  }
}

export function parseHostAccount(value: unknown): HostAccount | null {
  const source = record(value);
  const label = text(source?.label);
  if (!source || !label) return null;
  const accountURL = webURL(source.account_url);
  const logoutURL = webURL(source.logout_url);
  return {
    label,
    detail: text(source.detail),
    accountURL,
    accountLabel: accountURL ? text(source.account_label) || "Account settings" : undefined,
    logoutURL,
    logoutLabel: logoutURL ? text(source.logout_label) || "Sign out" : undefined,
  };
}

export async function loadHostAccount(endpoint: string): Promise<HostAccount | null> {
  const response = await fetch(endpoint, {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) return null;
  return parseHostAccount(await response.json());
}
