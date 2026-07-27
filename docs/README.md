# bitbucket-cli documentation

This is the [Starlight](https://starlight.astro.build/) documentation site for
[`bitbucket-cli`](https://github.com/thaodangspace/bitbucket-cli).

## Local development

```bash
npm ci
npm run dev
```

Build and preview the production site locally with:

```bash
npm run build
npm run preview
```

The static site is written to `dist/`.

## Cloudflare Pages

Configure a Pages project connected to this repository with:

- **Root directory:** `docs`
- **Build command:** `npm run build`
- **Build output directory:** `dist`
- **Node.js version:** use a supported LTS version, or set `NODE_VERSION`

Cloudflare Pages installs dependencies from `package-lock.json`. No Cloudflare
adapter or runtime functions are needed because Astro emits static HTML. Set
`SITE_URL` to the canonical public URL if you want Astro to emit a sitemap; it
is optional for deployment.

If the Pages project uses the repository root as its root directory instead:

- **Build command:** `npm --prefix docs ci && npm --prefix docs run build`
- **Build output directory:** `docs/dist`
