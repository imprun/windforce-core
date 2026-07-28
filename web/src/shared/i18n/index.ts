import i18next, { type TOptions } from "i18next";
import { useSyncExternalStore } from "react";
import { initReactI18next } from "react-i18next";
import { en, ko, type TranslationKey } from "./resources";

export type { TranslationKey } from "./resources";

export const supportedLocales = ["en", "ko"] as const;
export type Locale = (typeof supportedLocales)[number];

export const localeStorageKey = "wf.locale";

export function normalizeLocale(value: string | null | undefined): Locale | null {
  if (!value) return null;
  const language = value.trim().toLowerCase().split(/[-_]/, 1)[0];
  return language === "en" || language === "ko" ? language : null;
}

export function resolveInitialLocale({
  stored,
  browserLanguages,
}: {
  stored?: string | null;
  browserLanguages?: readonly string[];
} = {}): Locale {
  const storedLocale = normalizeLocale(stored);
  if (storedLocale) return storedLocale;
  for (const language of browserLanguages || []) {
    const locale = normalizeLocale(language);
    if (locale) return locale;
  }
  return "en";
}

function readStoredLocale(): string | null {
  try {
    return globalThis.localStorage?.getItem(localeStorageKey) || null;
  } catch {
    return null;
  }
}

function browserLanguages(): readonly string[] {
  if (typeof navigator === "undefined") return [];
  return navigator.languages?.length ? navigator.languages : [navigator.language];
}

function applyDocumentLanguage(locale: Locale) {
  if (typeof document !== "undefined") document.documentElement.lang = locale;
}

const initialLocale = resolveInitialLocale({
  stored: readStoredLocale(),
  browserLanguages: browserLanguages(),
});

void i18next.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    ko: { translation: ko },
  },
  lng: initialLocale,
  fallbackLng: "en",
  supportedLngs: supportedLocales,
  load: "languageOnly",
  initAsync: false,
  interpolation: {
    escapeValue: false,
  },
  returnNull: false,
});

applyDocumentLanguage(initialLocale);

export const i18n = i18next;

export function currentLocale(): Locale {
  return normalizeLocale(i18n.resolvedLanguage || i18n.language) || "en";
}

export function translate(key: TranslationKey, options?: TOptions): string {
  return i18n.t(key, options);
}

export async function setLocale(locale: Locale): Promise<void> {
  try {
    globalThis.localStorage?.setItem(localeStorageKey, locale);
  } catch {
    // Locale still changes for the current session when storage is unavailable.
  }
  applyDocumentLanguage(locale);
  await i18n.changeLanguage(locale);
}

function subscribeLocale(listener: () => void) {
  i18n.on("languageChanged", listener);
  return () => i18n.off("languageChanged", listener);
}

export function useLocale(): Locale {
  return useSyncExternalStore(subscribeLocale, currentLocale, () => "en");
}

export function localeTag(locale = currentLocale()): "en-US" | "ko-KR" {
  return locale === "ko" ? "ko-KR" : "en-US";
}
