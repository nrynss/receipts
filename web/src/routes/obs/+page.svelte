<script lang="ts">
  import { onMount } from 'svelte';
  import Overlay from '$lib/overlay/Overlay.svelte';
  import { decodeSnapshot } from '$lib/api/contracts';
  import { displayFromSnapshot, type Display } from '$lib/overlay/model';
  let display = $state<Display>({state:'idle'});
  let paired = $state(false);
  let code = $state('');
  let status = $state('Checking pairing…');
  onMount(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    let events: EventSource | undefined;
    let generation = 0;
    async function update() {
      const current = ++generation;
      try { const response = await fetch('/api/overlay'); const next = response.ok ? displayFromSnapshot(decodeSnapshot(await response.json())) : {state:'idle'} as Display; if (!stopped && current === generation) display = next; } catch { if (!stopped && current === generation) display = {state:'idle'}; }
    }
    function activate() {
      paired = true; void update(); events = new EventSource('/api/events');
      events.onopen = () => { void update(); };
      for (const name of ['snapshot','stamp','status','reset']) events.addEventListener(name, () => { void update(); });
      events.onerror = () => { generation++; display = {state:'idle'}; };
    }
    async function check() {
      try {
        const existing = await fetch('/auth/device');
        if (existing.ok) { status=''; activate(); return; }
        const start = await fetch('/auth/pair/start',{method:'POST'});
        if (!start.ok) throw new Error();
        code = (await start.json()).code; status='Enter this code in your Receipts setup.';
        async function poll() {
          if (stopped) return;
          const result = await fetch('/auth/pair/status');
          if (result.status === 204) { code=''; status=''; activate(); return; }
          if (result.status !== 202) { code=''; status='Code expired. Refresh this source to try again.'; return; }
          timer=setTimeout(poll,2000);
        }
        timer=setTimeout(poll,2000);
      } catch { status='Pairing unavailable. Refresh this source to try again.'; }
    }
    void check(); return () => { stopped=true; clearTimeout(timer); events?.close(); };
  });
</script>
<svelte:head><title>Receipts OBS source</title></svelte:head>
<main>{#if paired}<Overlay {display} />{:else}<p>{status}</p>{#if code}<strong>{code}</strong>{/if}{/if}</main>
<style>main{font:1.3rem system-ui;color:white;text-shadow:0 1px 4px black;padding:2rem;min-height:100vh;box-sizing:border-box;display:flex;align-items:flex-end;justify-content:center}strong{font-size:2.5rem;letter-spacing:.12em}</style>
