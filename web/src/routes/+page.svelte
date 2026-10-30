<script lang="ts">
  import { onMount } from 'svelte';
  import Overlay from '$lib/overlay/Overlay.svelte';
  import { fixture } from '$lib/overlay/fixtures';
  import type { Display, OverlayState } from '$lib/overlay/model';
  let display = $state<Display>({ state: 'idle' });
  onMount(() => {
    const selected = new URLSearchParams(window.location.search).get('fixture');
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
<main><Overlay {display} /></main>
<style>
  :global(html), :global(body) { margin:0; background:transparent; }
  main { min-height:100vh; display:flex; align-items:flex-end; justify-content:center; padding:3vw; box-sizing:border-box; }
</style>
