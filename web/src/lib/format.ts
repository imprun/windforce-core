import { localeTag } from "../shared/i18n";

export function shortSHA(value: string | null | undefined, length = 10): string {
  if (!value) return "—";
  return value.length > length ? value.slice(0, length) : value;
}

export function formatTime(value: string | null | undefined): string {
  if (!value || value.startsWith("0001-01-01")) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString(localeTag(), {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function formatRelative(value: string | null | undefined): string {
  if (!value || value.startsWith("0001-01-01")) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const deltaSeconds = Math.round((date.getTime() - Date.now()) / 1000);
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ["year", 31536000],
    ["month", 2592000],
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ];
  const formatter = new Intl.RelativeTimeFormat(localeTag(), { numeric: "auto" });
  for (const [unit, seconds] of units) {
    if (Math.abs(deltaSeconds) >= seconds) {
      return formatter.format(Math.trunc(deltaSeconds / seconds), unit);
    }
  }
  return formatter.format(deltaSeconds, "second");
}

export function formatDuration(ms: number | null | undefined): string {
  if (ms == null || ms < 0) return "—";
  const locale = localeTag();
  const formatUnit = (value: number, unit: Intl.NumberFormatOptions["unit"]) =>
    new Intl.NumberFormat(locale, {
      style: "unit",
      unit,
      unitDisplay: "short",
      maximumFractionDigits: 1,
    }).format(value);
  if (ms < 1000) return formatUnit(ms, "millisecond");
  const seconds = ms / 1000;
  if (seconds < 60) return formatUnit(seconds, "second");
  const minutes = Math.floor(seconds / 60);
  const rest = Math.round(seconds % 60);
  return `${formatUnit(minutes, "minute")} ${formatUnit(rest, "second")}`;
}

export function formatJSON(value: unknown): string {
  if (value === undefined) return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
