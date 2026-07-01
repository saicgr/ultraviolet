// Package i18n is a tiny key→{locale: text} dictionary used by the API for
// system-emitted strings (alert subjects, error responses' "user-friendly"
// fallback). Frontend i18n lives in frontend/src/lib/i18n.ts.
package i18n

import "strings"

var dict = map[string]map[string]string{
	"alert.freshness.subject": {
		"en": "Table %s is stale",
		"de": "Tabelle %s ist veraltet",
		"es": "La tabla %s está desactualizada",
		"fr": "La table %s est obsolète",
		"ja": "テーブル %s が古くなっています",
	},
	"error.unauthorized": {
		"en": "Authentication required.",
		"de": "Authentifizierung erforderlich.",
		"es": "Autenticación requerida.",
		"fr": "Authentification requise.",
		"ja": "認証が必要です。",
	},
}

// T returns the localized string for key+locale, falling back to en, then key.
func T(locale, key string) string {
	locale = strings.ToLower(strings.SplitN(locale, "-", 2)[0])
	if locale == "" {
		locale = "en"
	}
	if entry, ok := dict[key]; ok {
		if v, ok := entry[locale]; ok {
			return v
		}
		if v, ok := entry["en"]; ok {
			return v
		}
	}
	return key
}

// Locales returns the supported list. Drives the language picker.
func Locales() []string { return []string{"en", "de", "es", "fr", "ja"} }

// Messages returns every key→text for a locale (falling back to en per key).
// Backs GET /api/v1/i18n/{locale}/messages.json so the frontend's remote
// dictionary fetch resolves to a 200 instead of a 404.
func Messages(locale string) map[string]string {
	locale = strings.ToLower(strings.SplitN(locale, "-", 2)[0])
	if locale == "" {
		locale = "en"
	}
	out := make(map[string]string, len(dict))
	for key, entry := range dict {
		if v, ok := entry[locale]; ok {
			out[key] = v
		} else if v, ok := entry["en"]; ok {
			out[key] = v
		}
	}
	return out
}
