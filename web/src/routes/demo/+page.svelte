<script lang="ts">
  type Replay = { stage: 'ready' | 'complete'; claim?: string; verdict?: string; clip_status?: string; lore?: string; events?: string[] };
  let busy = $state(false);
  let error = $state('');
  let result = $state<Replay>({ stage: 'ready' });

  async function action(name: 'replay' | 'reset') {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const response = await fetch(`/demo/api/${name}`, { method: 'POST', credentials: 'same-origin' });
      if (!response.ok) throw new Error((await response.text()) || 'The demo is temporarily unavailable.');
      result = await response.json() as Replay;
    } catch (e) {
      error = e instanceof Error ? e.message : 'The demo is temporarily unavailable.';
    } finally {
      busy = false;
    }
  }
</script>

<svelte:head>
  <title>Recorded demo · Receipts</title>
  <meta name="description" content="Try the Receipts recorded demo without signing in or connecting a provider." />
</svelte:head>

<main>
  <article>
    <p class="eyebrow">RECORDED DEMO · NO LOGIN · NO PROVIDER CALLS</p>
    <h1>The stream remembers what you said.</h1>
    <p class="intro">Try one recorded promise through capture, contradiction detection, an appeal vote, and lore. This demo uses isolated, disposable state for your browser.</p>
    <div class="actions">
      <button disabled={busy} onclick={() => action('replay')}>{busy ? 'Replaying…' : 'Replay scenario'}</button>
      <button class="secondary" disabled={busy} onclick={() => action('reset')}>Reset demo</button>
    </div>
    <p class="status" aria-live="polite">{error || (result.stage === 'complete' ? 'Replay complete. Reset to try it again.' : 'Ready. Visitor data expires automatically.')}</p>
    {#if result.stage === 'complete'}
      <ol class="steps" aria-label="Recorded scenario results">
      {#each result.events ?? [] as event (event)}<li>{event}</li>{/each}
      </ol>
      <section aria-labelledby="receipt-title">
        <p class="eyebrow">THE PROMISE</p><h2 id="receipt-title">“{result.claim}”</h2>
        <p>Recorded clip result: <strong>{result.clip_status}</strong></p>
        <p>Recorded verdict: <strong>{result.verdict}</strong></p>
      </section>
      <section aria-labelledby="lore-title"><p class="eyebrow">CHANNEL LORE</p><h2 id="lore-title">{result.lore}</h2></section>
    {/if}
    <p class="note">Scenario: “I will never take that shortcut again.” The recorded broadcaster later takes the shortcut; chat upholds the appeal.</p>
  </article>
</main>

<style>
  :global(html), :global(body) { margin:0; min-height:100%; background:#101713; color:#f4eedc; font-family:system-ui,sans-serif; }
  main { min-height:100vh; display:grid; place-items:center; padding:32px 18px; box-sizing:border-box; }
  article { width:min(100%,760px); padding:clamp(22px,5vw,48px); border:2px solid #b9b3a5; background:#1a241e; box-shadow:10px 10px 0 #080b09; box-sizing:border-box; }
  .eyebrow { color:#f1b969; font-size:12px; font-weight:900; letter-spacing:.16em; }
  h1 { font-size:clamp(34px,7vw,64px); line-height:1; margin:20px 0; }
  .intro { max-width:60ch; line-height:1.6; color:#d4d0c5; }
  .actions { display:flex; flex-wrap:wrap; gap:12px; margin:26px 0 14px; }
  button { border:0; border-radius:6px; background:#f1b969; color:#101713; padding:12px 18px; font:inherit; font-weight:800; cursor:pointer; }
  button.secondary { background:transparent; color:#f4eedc; outline:1px solid #b9b3a5; }
  button:focus-visible { outline:3px solid #fff; outline-offset:3px; }
  button:disabled { opacity:.65; cursor:wait; }
  .status { min-height:1.5em; color:#d4d0c5; }
  .steps { padding-left:24px; line-height:1.8; }
  section { margin-top:24px; padding:20px; background:#101713; overflow-wrap:anywhere; }
  section h2 { margin:8px 0; font-size:clamp(20px,4vw,30px); }
  .note { margin-top:28px; color:#b9b3a5; font-size:14px; line-height:1.5; }
</style>
