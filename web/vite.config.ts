import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({ plugins: [{
  name: 'overlay-lifecycle-harness',
  apply: 'serve',
  configureServer(server) {
    server.middlewares.use('/tests/overlay.html', (_req, res) => {
      res.setHeader('Content-Type', 'text/html');
      res.end(`<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="frame-src 'none'"><title>Overlay lifecycle checks</title></head><body><main id="result">Running mounted checks…</main><div id="target"></div><script type="module" src="/src/lib/overlay/testing/overlay-checks.ts"></script></body></html>`);
    });
  }
}, sveltekit()] });
