import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  localeStorageKey,
  normalizeLocale,
  resolveInitialLocale,
  setLocale,
  supportedLocales,
} from ".";
import { en, ko } from "./resources";

const storedValues = new Map<string, string>();
const localStorageStub = {
  clear: () => storedValues.clear(),
  getItem: (key: string) => storedValues.get(key) ?? null,
  key: (index: number) => [...storedValues.keys()][index] ?? null,
  get length() {
    return storedValues.size;
  },
  removeItem: (key: string) => storedValues.delete(key),
  setItem: (key: string, value: string) => {
    storedValues.set(key, value);
  },
} as unknown as Storage;
const documentStub = { documentElement: { lang: "" } } as Document;

describe("locale resolution", () => {
  beforeEach(() => {
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: localStorageStub,
    });
    Object.defineProperty(globalThis, "document", {
      configurable: true,
      value: documentStub,
    });
  });

  afterEach(async () => {
    await setLocale("en");
    localStorageStub.clear();
    documentStub.documentElement.lang = "";
    Reflect.deleteProperty(globalThis, "localStorage");
    Reflect.deleteProperty(globalThis, "document");
  });

  it("prefers an explicit stored locale over browser languages", () => {
    expect(resolveInitialLocale({ stored: "ko", browserLanguages: ["en-US"] })).toBe("ko");
  });

  it("uses the first supported browser language and falls back to English", () => {
    expect(resolveInitialLocale({ browserLanguages: ["ja-JP", "ko-KR", "en-US"] })).toBe("ko");
    expect(resolveInitialLocale({ browserLanguages: ["ja-JP"] })).toBe("en");
  });

  it("normalizes regional and underscore locale forms", () => {
    expect(normalizeLocale("KO-kr")).toBe("ko");
    expect(normalizeLocale("en_US")).toBe("en");
    expect(normalizeLocale("fr-FR")).toBeNull();
  });

  it("persists a locale and updates the document language", async () => {
    await setLocale("ko");
    expect(localStorageStub.getItem(localeStorageKey)).toBe("ko");
    expect(documentStub.documentElement.lang).toBe("ko");
  });
});

describe("translation catalogs", () => {
  it("have exact English and Korean key parity", () => {
    expect(supportedLocales).toEqual(["en", "ko"]);
    expect(Object.keys(ko).sort()).toEqual(Object.keys(en).sort());
  });

  it("do not contain blank translations", () => {
    expect(Object.values(en).every((value) => value.trim().length > 0)).toBe(true);
    expect(Object.values(ko).every((value) => value.trim().length > 0)).toBe(true);
  });

  it("renders every catalog key in both supported locales without exposing a missing key", () => {
    for (const key of Object.keys(en) as Array<keyof typeof en>) {
      expect(en[key]).not.toBe(key);
      expect(ko[key]).not.toBe(key);
    }
  });

  it("localizes representative UI, validation, status, and API error copy", () => {
    expect(ko["navigation.apps"]).toBe("앱");
    expect(ko["trigger.validation.nameRequired"]).toBe("이름은 필수입니다.");
    expect(ko["webhook.status.failed"]).toBe("실패");
    expect(ko["apiError.forbidden"]).toBe("이 작업을 수행할 권한이 없습니다.");
  });
});
