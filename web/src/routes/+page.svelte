<script lang="ts">
  import { resolve } from '$app/paths';
  import { onMount } from 'svelte';
  import Overlay from '$lib/overlay/Overlay.svelte';
  import { fixture } from '$lib/overlay/fixtures';
  import type { Display, OverlayState } from '$lib/overlay/model';
  let display = $state<Display>({ state: 'idle' });
  let showSetup = $state(true);
  onMount(() => {
    const selected = new URLSearchParams(window.location.search).get('fixture');
    showSetup = !selected;
    if (selected === 'sequence') {
      const states: OverlayState[] = ['stamp', 'evidence', 'appeal', 'verdict', 'idle'];
      let index = 0; display = fixture(states[index]);
      const timer = window.setInterval(() => { index++; display = fixture(states[index] ?? 'idle'); if (index >= states.length - 1) window.clearInterval(timer); }, 5000);
      return () => window.clearInterval(timer);
    }
    if (selected && ['idle','stamp','evidence','appeal','verdict'].includes(selected)) display = fixture(selected as OverlayState);
  });
</script>
<svelte:head><meta name="description" content="The stream remembers what you said." /></svelte:head>
<main>{#if showSetup}<nav><a href={resolve('/demo/')}>Try the recorded demo</a><button onclick={() => { window.location.href = "/account"; }}>Set up Receipts</button></nav>{/if}<Overlay {display} /></main>
<style>
  :global(html), :global(body) { margin:0; background:transparent; }
  main { min-height:100vh; display:flex; align-items:flex-end; justify-content:center; padding:3vw; box-sizing:border-box; }
  nav { position:fixed; top:16px; right:16px; display:flex; gap:12px; align-items:center; font:14px system-ui,sans-serif; }
  nav a { color:#f1b969; }
  nav button { font:inherit; }
</style>
