export interface HostConsoleConfig {
  url: string;
  label: string;
}

export function parseHostConsoleConfig(value: unknown): HostConsoleConfig | null {
  if (!value || typeof value !== "object" || !("host_console" in value)) return null;
  const hostConsole = value.host_console;
  if (!hostConsole || typeof hostConsole !== "object") return null;
  if (!("url" in hostConsole) || !("label" in hostConsole)) return null;
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

export async function loadHostConsoleConfig(): Promise<HostConsoleConfig | null> {
  const response = await fetch("/ui/config.json", {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) return null;
  return parseHostConsoleConfig(await response.json());
}
