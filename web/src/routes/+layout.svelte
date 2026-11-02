<script lang="ts">
  import { page } from '$app/state';
  import { resolve } from '$app/paths';
  import ReceiptMark from '$lib/brand/ReceiptMark.svelte';
  import '../lib/ui/tokens.css';

  let { children } = $props();
  const humanRoutes = ['/', '/demo/', '/account', '/control'];
  let showShell = $derived(humanRoutes.includes(page.url.pathname) && !(page.url.pathname === '/' && page.url.searchParams.has('fixture')));
</script>

{#if showShell}
  <div class="app-shell">
    <header class="site-header">
      <div class="site-header__inner">
        <a class="site-brand" href={resolve('/')} aria-label="Receipts home">
          <ReceiptMark size={32} />
          <span>RECEIPTS</span>
        </a>
        <nav class="site-nav" aria-label="Main navigation">
          <a href={resolve('/')} aria-current={page.url.pathname === '/' ? 'page' : undefined}>Home</a>
          <a href={resolve('/demo/')} aria-current={page.url.pathname === '/demo/' ? 'page' : undefined}>Demo</a>
          <a href={resolve('/account')} aria-current={page.url.pathname === '/account' ? 'page' : undefined}>Account</a>
          <a href={resolve('/control')} aria-current={page.url.pathname === '/control' ? 'page' : undefined}>Control</a>
          <a href="https://nrynss.github.io/receipts/" target="_blank" rel="noreferrer">Docs</a>
        </nav>
      </div>
    </header>
    {@render children()}
  </div>
{:else}
  {@render children()}
{/if}
