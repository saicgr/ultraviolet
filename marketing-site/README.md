# marketing-site

The Ultraviolet public website (landing, pricing, docs portal, blog) lives in
its own Vercel project.

## Quick start

```bash
cd marketing-site
npx create-next-app@latest --ts --tailwind --app
```

## Pages to ship before public launch

- `/` — hero ("BI that learns from every query."), feature grid, CTA, social proof.
- `/pricing` — Free / Team / Business / Enterprise tiers with feature matrix
  derived from `internal/billing.FeatureFlags`.
- `/docs` — Mintlify or Docusaurus, generated from `docs/` + the `openapi.yaml`.
- `/blog` — MDX, Vercel hosted.
- `/security` — links to status page, SOC 2 report, data-residency map.

## Why a separate repo / project

The product app (`frontend/`) is auth-gated, behind login. The marketing site is
unauth, indexable, ships independently — separate Vercel projects, separate
deploys, no risk of accidentally exposing the app build to crawlers.

See `gtm-1`, `gtm-2`, `gtm-3` in AUDIT.md.
