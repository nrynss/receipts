<script lang="ts">
  import { onMount } from 'svelte';
  import { resolve } from '$app/paths';
  import { getControl, createActions } from '$lib/control/client';
  import { fixtureFetch } from '$lib/control/fixture';
  let request: typeof fetch = fetch;
  import type { OverlaySnapshot, ReviewItem, Health } from '$lib/api/contracts';
  let statusValue = $state('unavailable');
  let canStart = $state(false);
  let snapshot = $state<OverlaySnapshot | null>(null);
  let reviews = $state<ReviewItem[]>([]);
  let health = $state<Health | null>(null);
  let available = $state(false);
  let busy = $state(false);
  let message = $state('');
  let reason = $state('');
  let voidID = $state('');
  let actions: ReturnType<typeof createActions>;
  let listening = $derived(statusValue === 'listening' || statusValue === 'paused');
  async function load() {
    try {
      const data = await getControl(request);
      actions.setScope(data.owner.id, data.owner.channel_id);
      statusValue = data.state.state; canStart = data.state.can_start === true;
      snapshot = data.snapshot; reviews = data.reviews; health = data.health; available = data.runtimeAvailable;
    } catch (error) { message = error instanceof Error ? error.message : 'Controls unavailable.'; }
  }
  async function act(path: string, body: Record<string, unknown> = {}, keyed = false) {
    if (busy) return;
    busy = true; message = '';
    try { await actions.send(path, body, keyed); message = 'Action applied.'; await load(); }
    catch (error) { message = error instanceof Error ? error.message : 'Action failed. Try again.'; }
    finally { busy = false; }
  }
  function answer(decision: string) {
    if (!snapshot?.current_stamp) return;
    if (decision === 'context_changed' && !reason.trim()) { message = 'Enter a reason for the change in context.'; return; }
    void act('/api/control/decision', { receipt_id: snapshot.current_stamp.id, receipt_decision: decision, ...(decision === 'context_changed' ? { note: reason.trim() } : {}) }, true);
  }
  onMount(() => { if (import.meta.env.DEV && new URLSearchParams(window.location.search).get('fixture') === 'dock') request = fixtureFetch(); actions = createActions(request, () => document.cookie, undefined, sessionStorage); void load(); });
</script>
<svelte:head><title>Receipts control dock</title></svelte:head>
<main>
  {#if import.meta.env.DEV && typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('fixture') === 'dock'}<p>Fixture rehearsal · no live actions</p>{/if}
  <header><div><p class="eyebrow">PRIVATE DOCK</p><h1>Receipts</h1></div><button onclick={() => load()} disabled={busy}>Refresh</button></header>
  <section aria-labelledby="status-heading"><h2 id="status-heading">Stream status</h2>
    <div class="status"><p><span>Live</span><strong>{health ? (health.live_confirmed ? 'Confirmed' : 'Not confirmed') : 'Unknown'}</strong></p><p><span>Audio</span><strong>{listening ? 'Listening' : statusValue === 'not_listening' ? 'Not listening' : statusValue.replaceAll('_', ' ')}</strong></p><p><span>Stamps</span><strong>{listening ? (statusValue === 'paused' ? 'Paused' : 'On') : 'Unavailable'}</strong></p></div>
    {#if listening}<div class="actions"><button disabled={busy} onclick={() => act('/api/control/pause', { paused: statusValue !== 'paused' })}>{statusValue === 'paused' ? 'Resume stamps' : 'Pause stamps'}</button><button disabled={busy} onclick={() => act('/account-api/stop')}>Stop listening</button></div>
    {:else if statusValue === 'not_listening' && canStart}<button disabled={busy} onclick={() => act('/account-api/start')}>Start listening</button>
    {:else}<p class="muted">Starting listening is currently unavailable. Check setup and live status.</p>{/if}
  </section>
  <section aria-labelledby="stamp-heading"><h2 id="stamp-heading">Current stamp</h2>
    {#if snapshot?.current_stamp}<p class="claim">{snapshot.current_stamp.claim}</p><p class="muted">Receipt {snapshot.current_stamp.id}</p>
      <div class="actions"><button disabled={busy} onclick={() => answer('admit')}>Admit</button><button disabled={busy} onclick={() => answer('appeal')}>Appeal</button></div>
      <label for="reason">Why did the context change?</label><textarea id="reason" bind:value={reason} disabled={busy} rows="3"></textarea><button disabled={busy} onclick={() => answer('context_changed')}>Context changed</button>
    {:else}<p class="muted">{available ? 'No stamp awaiting an answer.' : 'Current stamp is unavailable while the runtime is inactive.'}</p>{/if}
    {#if snapshot?.current_appeal}<p>Appeal open · {snapshot.current_appeal.uphold_votes} uphold / {snapshot.current_appeal.overturn_votes} overturn</p>{/if}
  </section>
  <section aria-labelledby="review-heading"><h2 id="review-heading">Quiet review</h2><p class="muted">These candidates are private until approved.</p>
    {#each reviews as item (item.id)}<article><p class="claim">{item.claim}</p><p>{item.reason}</p><div class="actions"><button disabled={busy} onclick={() => act('/api/control/review', { review_id: item.id, decision: 'approve' }, true)}>Approve</button><button disabled={busy} onclick={() => act('/api/control/review', { review_id: item.id, decision: 'reject' }, true)}>Reject</button></div></article>{:else}<p class="muted">{available ? 'No items to review.' : 'Review is unavailable while the runtime is inactive.'}</p>{/each}
  </section>
  <section aria-labelledby="void-heading"><h2 id="void-heading">Void a receipt</h2><form onsubmit={(event) => { event.preventDefault(); void act('/api/control/void', { receipt_id: voidID.trim() }); }}><label for="void-id">Receipt ID</label><input id="void-id" bind:value={voidID} required disabled={busy} /><button disabled={busy || !voidID.trim()}>Void receipt</button></form></section>
  <p class="muted">If an answer fails, retry the same choice and reason. The retry identity survives a refresh in this tab.</p>
  <p role="status" aria-live="polite">{busy ? 'Applying action…' : message}</p><p><a href={resolve("/account")}>Account setup</a></p>
</main>
<style>
:global(body){margin:0;background:#101318;color:#f4f2ec;font:16px/1.45 system-ui,sans-serif}main{max-width:38rem;margin:auto;padding:1rem}header{display:flex;align-items:center;justify-content:space-between}h1{font-size:1.7rem;margin:0}h2{font-size:1.1rem;margin:0 0 .7rem}.eyebrow{color:#f0c36a;font-size:.7rem;letter-spacing:.16em;margin:0}section{background:#1b2028;border:1px solid #39414c;border-radius:.65rem;padding:1rem;margin:1rem 0}.status{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}.status p{margin:0 0 .8rem}.status span,.status strong{display:block}.status span,.muted{color:#bcc5d0}.status strong{text-transform:capitalize}.claim{font-size:1.1rem;font-weight:650;overflow-wrap:anywhere}button,input,textarea{font:inherit}button{background:#f0c36a;color:#151515;border:0;border-radius:.35rem;padding:.55rem .8rem;font-weight:650;cursor:pointer}button:disabled{opacity:.5;cursor:wait}button:focus-visible,input:focus-visible,textarea:focus-visible,a:focus-visible{outline:3px solid #fff;outline-offset:3px}.actions{display:flex;flex-wrap:wrap;gap:.5rem}label{display:block;margin:.8rem 0 .3rem}input,textarea{box-sizing:border-box;width:100%;background:#101318;color:#fff;border:1px solid #8994a1;border-radius:.3rem;padding:.5rem}textarea{margin-bottom:.5rem}article{border-top:1px solid #39414c;padding:.5rem 0}form button{margin-top:.5rem}a{color:#f0c36a}
</style>
