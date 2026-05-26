// ux-4 — Lightweight i18n wiring for the control plane.
//
// Tries `GET /api/v1/i18n/{locale}/messages.json` first; falls back to the
// hardcoded `en` dictionary below if the request fails (the backend
// `internal/i18n` package is wired up, but the endpoint may be missing in
// some deployments — TODO: remove fallback once the route ships in all envs).
//
// Locale choice persists in localStorage under `uv_locale`.

import * as React from "react";
import { api } from "../lib/api";

export type Locale = "en" | "es" | "fr" | "de" | "ja";

export const SUPPORTED_LOCALES: { code: Locale; label: string }[] = [
  { code: "en", label: "English" },
  { code: "es", label: "Español" },
  { code: "fr", label: "Français" },
  { code: "de", label: "Deutsch" },
  { code: "ja", label: "日本語" },
];

const FALLBACK_EN: Record<string, string> = {
  "nav.savings": "Savings",
  "nav.connections": "Connections",
  "nav.sync": "Sync",
  "nav.queries": "Queries",
  "nav.catalog": "Catalog",
  "nav.lineage": "Lineage",
  "nav.dashboards": "Dashboards",
  "nav.workbench": "Workbench",
  "nav.impact": "Impact",
  "nav.agents": "Agents",
  "nav.activity": "Activity",
  "nav.inbox": "Inbox",
  "nav.subscriptions": "Subscriptions",
  "nav.webhooks": "Webhooks",
  "nav.scheduled_queries": "Scheduled Queries",
  "nav.access_reviews": "Access Reviews",
  "nav.copilot": "Copilot",
  "nav.narrator": "Narrator",
  "nav.watches": "Lineage Watches",
  "nav.api_keys": "API Keys",
  "nav.connect": "Connect",
  "common.loading": "Loading…",
  "common.empty": "No data yet",
};

const LS_KEY = "uv_locale";

export function getInitialLocale(): Locale {
  if (typeof localStorage === "undefined") return "en";
  const v = localStorage.getItem(LS_KEY) as Locale | null;
  return v && SUPPORTED_LOCALES.some((l) => l.code === v) ? v : "en";
}

export function setLocale(l: Locale) {
  if (typeof localStorage !== "undefined") localStorage.setItem(LS_KEY, l);
}

type Ctx = { locale: Locale; messages: Record<string, string>; setLocale: (l: Locale) => void };

const I18nContext = React.createContext<Ctx>({
  locale: "en",
  messages: FALLBACK_EN,
  setLocale: () => {},
});

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = React.useState<Locale>(getInitialLocale());
  const [messages, setMessages] = React.useState<Record<string, string>>(FALLBACK_EN);

  React.useEffect(() => {
    let cancelled = false;
    api
      .i18nMessages(locale)
      .then((m) => {
        if (!cancelled && m && Object.keys(m).length) setMessages({ ...FALLBACK_EN, ...m });
      })
      .catch(() => {
        if (!cancelled) setMessages(FALLBACK_EN);
      });
    return () => {
      cancelled = true;
    };
  }, [locale]);

  const value = React.useMemo<Ctx>(
    () => ({
      locale,
      messages,
      setLocale: (l: Locale) => {
        setLocale(l);
        setLocaleState(l);
      },
    }),
    [locale, messages],
  );
  return React.createElement(I18nContext.Provider, { value }, children);
}

export function useI18n() {
  return React.useContext(I18nContext);
}

export function useT() {
  const { messages } = useI18n();
  return React.useCallback((key: string) => messages[key] ?? key, [messages]);
}
