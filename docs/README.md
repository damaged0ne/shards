# shards documentation

The shards documentation site, built with [Docusaurus](https://docusaurus.io/).

```bash
npm ci
npm start       # local development server with live reload
npm run build   # static site in ./build
```

By default the site is built for GitHub Pages at `https://damaged0ne.github.io/shards/`.
Set `DOCS_URL` and `DOCS_BASE_URL` (for example `DOCS_BASE_URL=/`) to host it elsewhere.
The `.github/workflows/docs.yml` workflow builds and deploys it on every push to `main` that touches `docs/`.
