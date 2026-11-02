import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
const base = process.env.SITE_BASE ?? '/receipts';
const origin = process.env.SITE_ORIGIN ?? 'https://nrynss.github.io';
export default defineConfig({ site: origin, base, integrations: [starlight({
  title: 'Receipts', description: 'A stream remembers what you said.', favicon: '/favicon.svg', customCss: ['./src/styles/receipts.css'],
  logo: { src: './src/assets/receipt-mark.svg', alt: 'Receipts mark' },
  social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/nrynss/receipts' }],
  sidebar: [
    { label: 'The match', items: [{ label: 'Overview', slug: 'index' }, { label: 'Judge walkthrough', slug: 'judge-walkthrough' }] },
    { label: 'Build and run', items: [{ label: 'Connect Twitch and OBS', slug: 'connect' }, { label: 'How it works', slug: 'how-it-works' }, { label: 'Built with Codex', slug: 'built-with-codex' }, { label: 'Run locally', slug: 'run-locally' }] },
    { label: 'Trust and evidence', items: [{ label: 'Safety and limits', slug: 'safety' }, { label: 'Evidence and evaluation', slug: 'evidence' }] },
  ],
  editLink: { baseUrl: 'https://github.com/nrynss/receipts/edit/main/site/' },
  head: [{ tag: 'meta', attrs: { name: 'theme-color', content: '#101713' } }],
})] });
