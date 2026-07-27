export interface HostConsoleConfig {
  url: string;
  label: string;
}

export interface HostAccountConfig {
  endpoint: string;
}

export interface RuntimeConfig {
  hostConsole: HostConsoleConfig | null;
  hostAccount: HostAccountConfig | null;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function parseHostConsole(value: unknown): HostConsoleConfig | null {
  const hostConsole = record(value);
  if (!hostConsole) return null;
  if (typeof hostConsole.url !== "string" || typeof hostConsole.label !== "string") return null;
  const label = hostConsole.label.trim();
  if (!label) return null;
  try {
    const target = new URL(hostConsole.url);
    if (!["http:", "https:"].includes(target.protocol) || !target.host) return null;
    return { url: target.toString(), label };
  } catch {
    return null;
  }
}

function parseHostAccount(value: unknown): HostAccountConfig | null {
  const hostAccount = record(value);
  if (!hostAccount || typeof hostAccount.endpoint !== "string") return null;
  const endpoint = hostAccount.endpoint.trim();
  if (!endpoint.startsWith("/") || endpoint.startsWith("//")) return null;
  try {
    const target = new URL(endpoint, "https://runtime.invalid");
    if (target.origin !== "https://runtime.invalid") return null;
    return { endpoint: `${target.pathname}${target.search}` };
  } catch {
    return null;
  }
}

export function parseRuntimeConfig(value: unknown): RuntimeConfig {
  const config = record(value);
  return {
    hostConsole: parseHostConsole(config?.host_console),
    hostAccount: parseHostAccount(config?.host_account),
  };
}

export function parseHostConsoleConfig(value: unknown): HostConsoleConfig | null {
  return parseRuntimeConfig(value).hostConsole;
}

export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  const response = await fetch("/ui/config.json", {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) return { hostConsole: null, hostAccount: null };
  return parseRuntimeConfig(await response.json());
}

export async function loadHostConsoleConfig(): Promise<HostConsoleConfig | null> {
  return (await loadRuntimeConfig()).hostConsole;
}
