// @ts-check
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'astro/config';

const repoRoot = fileURLToPath(new URL('..', import.meta.url));

// GitHub Pages serves the site at the root of its custom domain (DNS on Cloudflare).
export default defineConfig({
  site: 'https://flowlens.gofunc.site',
  trailingSlash: 'ignore',
  // Dev and preview servers stay on the IPv4 loopback address only.
  server: {
    host: '127.0.0.1',
  },
  i18n: {
    locales: ['en', 'zh-cn'],
    defaultLocale: 'en',
    routing: {
      prefixDefaultLocale: false,
    },
  },
  vite: {
    server: {
      // Screenshots and icons are imported from the repository, not copied.
      fs: { allow: [repoRoot] },
    },
  },
});
