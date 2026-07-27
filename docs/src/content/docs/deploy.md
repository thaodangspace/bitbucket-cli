---
title: Deploy to Cloudflare Pages
description: Publish the static bitbucket-cli documentation site with Cloudflare Pages.
---

The docs site is an Astro static build. It does not need a server adapter,
Cloudflare Worker, or runtime functions. Set `SITE_URL` to the canonical public
URL to enable sitemap generation; it is otherwise optional.

## Configure a Pages project

Connect the repository to Cloudflare Pages with:

| Setting | Value |
| --- | --- |
| Root directory | `docs` |
| Build command | `npm run build` |
| Build output directory | `dist` |

Cloudflare Pages installs dependencies from `docs/package-lock.json`, runs Astro,
and publishes the generated `dist/` directory. Use a supported Node.js LTS
version, setting `NODE_VERSION` when needed.

## Verify locally

```sh
cd docs
npm ci
npm run build
npm run preview
```

The root-level shortcut is:

```sh
make docs-build
```

:::tip[Repository-root alternative]
If the Pages project must keep the repository root as its root directory, use
`npm --prefix docs ci && npm --prefix docs run build` and publish `docs/dist`.
:::
