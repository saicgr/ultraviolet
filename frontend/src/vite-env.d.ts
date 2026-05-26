/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_BRAND_NAME?: string;
  readonly VITE_BRAND_TAGLINE?: string;
  readonly VITE_BRAND_DOMAIN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
