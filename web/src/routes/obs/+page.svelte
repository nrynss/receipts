<script lang="ts">
  import { onMount } from 'svelte';
  import Overlay from '$lib/overlay/Overlay.svelte';
  import { fixture } from '$lib/overlay/fixtures';
  import { decodeSnapshot } from '$lib/api/contracts';
  import { createOverlayRecovery } from '$lib/events/recovery';
  import { type Display } from '$lib/overlay/model';
  import ReceiptMark from '$lib/brand/ReceiptMark.svelte';
  let display = $state<Display>({state:'idle'});
  let paired = $state(false);
  let code = $state('');
  let status = $state('Checking pairing…');
  onMount(() => {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    let events: EventSource | undefined;
    let recovery: ReturnType<typeof createOverlayRecovery> | undefined;
    function activate() {
      if (stopped) return;
      paired = true;
      recovery = createOverlayRecovery(async () => {
        const response = await fetch('/api/overlay');
        if (!response.ok) throw new Error(`overlay snapshot unavailable (${response.status})`);
        return decodeSnapshot(await response.json());
      }, next => { if (!stopped) display = next; });
      events = new EventSource('/api/events');
      events.onopen = () => { if (!stopped) void recovery?.catchUp(); };
      for (const name of ['snapshot','stamp','status','reset']) events.addEventListener(name, event => {
        if (!stopped) recovery?.onFrame({ name, data: (event as MessageEvent<string>).data });
      });
      events.onerror = () => { if (!stopped) recovery?.disconnect(); };
    }
    async function check() {
      try {
        const existing = await fetch('/auth/device');
        if (stopped) return;
        if (existing.ok) { status=''; activate(); return; }
        const start = await fetch('/auth/pair/start',{method:'POST'});
        if (stopped) return;
        if (!start.ok) throw new Error();
        const started = await start.json();
        if (stopped) return;
        code = started.code; status='Enter this four-character code in Receipts setup. Expires in 2 minutes.';
        async function poll() {
          if (stopped) return;
          const result = await fetch('/auth/pair/status');
          if (stopped) return;
          if (result.status === 204) { code=''; status=''; activate(); return; }
          if (result.status !== 202) { code=''; status='Code expired. Refresh this source to try again.'; return; }
          timer=setTimeout(poll,2000);
        }
        timer=setTimeout(poll,2000);
      } catch { if (!stopped) status='Pairing unavailable. Refresh this source to try again.'; }
    }
    if (import.meta.env.DEV) { const mode = new URLSearchParams(window.location.search).get('fixture'); if (mode === 'pairing') { code='K7MX'; status='Enter this four-character code in Receipts setup. Expires in 2 minutes.'; return; } if (mode === 'paired') { paired=true; display=fixture('evidence'); return; } }
    void check(); return () => { stopped=true; clearTimeout(timer); events?.close(); recovery?.dispose(); };
  });
</script>
<svelte:head><title>Receipts OBS source</title></svelte:head>
<main class:paired>{#if paired}<Overlay {display} />{:else}<section class="pairing"><ReceiptMark size={72} /><p class="eyebrow">RECEIPTS · OBS OVERLAY</p><p>{status}</p>{#if code}<strong>{code}</strong>{/if}</section>{/if}</main>
<style>
  :global(html), :global(body) { margin:0; background:transparent; }
  main { box-sizing:border-box; width:100%; height:100vh; display:flex; align-items:center; justify-content:center; padding:2rem; font:1.3rem system-ui; }
  main.paired { align-items:flex-end; }
  .pairing { box-sizing:border-box; max-width:min(40rem,100%); padding:2rem 3rem; text-align:center; color:#f4eedc; background:#171d1a; border:3px solid #f1b969; box-shadow:8px 8px 0 #080b09; }
  .pairing :global(svg) { display:block; margin:0 auto .75rem; }
  .pairing p { margin:0 0 1rem; }
  .pairing .eyebrow { color:#f1b969; font-size:.8rem; font-weight:800; letter-spacing:.14em; }
  .pairing strong { display:block; font-size:clamp(1.5rem,4vw,3rem); letter-spacing:.12em; overflow-wrap:anywhere; }
</style>
