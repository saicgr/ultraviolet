// Centralized product display name for the frontend.
//
// Override at build time with Vite env vars:
//   VITE_BRAND_NAME="Acme"     VITE_BRAND_TAGLINE="..."     VITE_BRAND_DOMAIN="acme.io"
//
// Never inline the literal product name in components — always import from here.
// Backend mirror: internal/branding/branding.go.

export const BRANDING = {
  name: import.meta.env.VITE_BRAND_NAME ?? "Ultraviolet",
  tagline: import.meta.env.VITE_BRAND_TAGLINE ?? "The query cache for your warehouse.",
  domain: import.meta.env.VITE_BRAND_DOMAIN ?? "ultraviolet.dev",
} as const;
