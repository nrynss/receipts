import { readFile, readdir, stat } from 'node:fs/promises';
import { join, resolve } from 'node:path';

const root = resolve('dist');
const base = (process.env.SITE_BASE ?? '/receipts').replace(/\/$/, '');
const htmlFiles = [];
async function walk(dir) {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) await walk(path);
    else if (entry.name.endsWith('.html')) htmlFiles.push(path);
  }
}
await walk(root);
if (htmlFiles.length < 9) throw new Error(`Expected 9 rendered routes, found ${htmlFiles.length}`);
const missing = new Set();
for (const file of htmlFiles) {
  const html = await readFile(file, 'utf8');
  for (const [, url] of html.matchAll(new RegExp(`(?:href|src)="(${base}/[^"#?]+)"`, 'g'))) {
    const path = resolve(root, `.${decodeURIComponent(url.slice(base.length))}`);
    try { await stat(path); } catch { missing.add(`${url} from ${file}`); }
  }
  if (!html.includes('id="starlight__search"') && !file.endsWith('/404.html')) throw new Error(`Missing Starlight search markup: ${file}`);
}
if (missing.size) throw new Error(`Missing local assets or pages:\n${[...missing].join('\n')}`);
console.log(`Checked ${htmlFiles.length} HTML pages and base-prefixed local links.`);
