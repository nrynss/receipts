<script lang="ts">
  import { onMount } from 'svelte';
  import { statusRefresh } from '$lib/account/status-refresh';
  import { resolve } from '$app/paths';
  import { getControl, createActions } from '$lib/control/client';
  import { fixtureFetch } from '$lib/control/fixture';
  let request: typeof fetch = fetch;
  import type { OverlaySnapshot, ReviewItem, Health } from '$lib/api/contracts';
  let statusValue = $state('unavailable');
  let canStart = $state(false);
  let transcriptionEnabled = $state(false);
  let soleBroadcaster = $state(false);
  let uncertainSession = $state(false);
  let poll: ReturnType<typeof statusRefresh> | undefined;
  let statusVersion=0;
  let disposed=false;
  let snapshot = $state<OverlaySnapshot | null>(null);
  let reviews = $state<ReviewItem[]>([]);
  let health = $state<Health | null>(null);
  let available = $state(false);
  let busy = $state(false);
  let message = $state('');
  let reason = $state('');
  let voidID = $state('');
  let actions: ReturnType<typeof createActions>;
  let listening = $state(false);
  let now = $state(Date.now());
  let sessionActive = $derived(listening || statusValue === 'session_active' || uncertainSession);
  let verifiedLive = $derived(statusValue === 'unknown' || !health ? 'Unknown' : statusValue === 'offline' ? 'Offline' : health.live_confirmed ? 'Confirmed' : 'Not confirmed');
  let appealRemaining = $derived(snapshot?.current_appeal ? Math.max(0, Math.ceil((Date.parse(snapshot.current_appeal.closes_at) - now) / 1000)) : 0);
  let appealUpholdPercent = $derived(snapshot?.current_appeal ? (snapshot.current_appeal.uphold_votes + snapshot.current_appeal.overturn_votes ? snapshot.current_appeal.uphold_votes / (snapshot.current_appeal.uphold_votes + snapshot.current_appeal.overturn_votes) * 100 : 50) : 50);
  async function load() {
    const version=++statusVersion; poll?.cancel();
    try {
      const data = await getControl(request);
      if (disposed || version!==statusVersion) return;
      actions.setScope(data.owner.id, data.owner.channel_id);
      statusValue = data.state.state; canStart = data.state.can_start === true;
      transcriptionEnabled = data.state.transcription_enabled === true; uncertainSession=false;
      listening = data.state.audio_capture_active === true || statusValue === 'listening' || statusValue === 'paused';
      snapshot = data.snapshot; reviews = data.reviews; health = data.health; available = data.runtimeAvailable;
    } catch (error) { if (disposed || version!==statusVersion) return; uncertainSession=sessionActive;statusValue='unknown';listening=false;transcriptionEnabled=false;message = error instanceof Error ? error.message : 'Controls unavailable.'; }
  }
  function refreshStatus() { if (!busy) { statusVersion++; void poll?.run(); } }
  async function act(path: string, body: Record<string, unknown> = {}, keyed = false) {
    if (busy) return;
    poll?.cancel(); statusVersion++; busy = true; message = '';
    try { await actions.send(path, body, keyed); message = 'Action applied.'; await load(); }
    catch (error) { message = error instanceof Error ? error.message : 'Action failed. Try again.'; }
    finally { busy = false; }
  }
  async function startSession() {
    const attested = soleBroadcaster;
    soleBroadcaster = false;
    await act('/account-api/start', { sole_broadcaster: attested });
  }
  function answer(decision: string) {
    if (!snapshot?.current_stamp) return;
    if (decision === 'context_changed' && !reason.trim()) { message = 'Enter a reason for the change in context.'; return; }
    void act('/api/control/decision', { receipt_id: snapshot.current_stamp.id, receipt_decision: decision, ...(decision === 'context_changed' ? { note: reason.trim() } : {}) }, true);
  }
  onMount(() => { if (import.meta.env.DEV && new URLSearchParams(window.location.search).get('fixture') === 'dock') request = fixtureFetch(); actions = createActions(request, () => document.cookie, undefined, sessionStorage); poll=statusRefresh(request, (state) => { if (typeof state.state === 'string') statusValue=state.state;canStart=state.can_start===true;listening=state.audio_capture_active===true;transcriptionEnabled=state.transcription_enabled===true;uncertainSession=false; }, () => { uncertainSession=sessionActive;statusValue='unknown';listening=false;transcriptionEnabled=false; }); void load(); const countdownTimer=window.setInterval(() => { now=Date.now(); },1000); const statusTimer=window.setInterval(() => { void refreshStatus(); },5000); return () => { disposed=true;statusVersion++;poll?.dispose();window.clearInterval(countdownTimer);window.clearInterval(statusTimer); }; });
</script>
<svelte:head><title>Receipts control dock</title></svelte:head>
<main>
  {#if import.meta.env.DEV && typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('fixture') === 'dock'}<p>Fixture rehearsal · no live actions</p>{/if}
  <header><div><p class="eyebrow">STREAMER CONTROL DOCK</p><h1>Receipts</h1></div><button class="btn-secondary" onclick={() => load()} disabled={busy}>Refresh</button></header>
  <section aria-labelledby="status-heading"><h2 id="status-heading">Stream status</h2>
    <div class="status"><p><span>Stream</span><strong>{verifiedLive}</strong></p><p><span>Speech-to-Text</span><strong>{listening && transcriptionEnabled ? 'Active' : 'Off'}</strong></p><p><span>Stamps</span><strong>{listening ? (statusValue === 'paused' ? 'Paused' : 'Manual review enabled') : 'Unavailable'}</strong></p></div>
    {#if listening}<p>{transcriptionEnabled ? 'Solo broadcast confirmed · Speech-to-text attribution is active.' : 'Speech-to-text is paused for this session because sole-speaker confirmation was not checked.'}</p><div class="actions"><button class="btn-secondary" disabled={busy} onclick={() => act('/api/control/pause', { paused: statusValue !== 'paused' })}>{statusValue === 'paused' ? 'Resume stamps' : 'Pause stamps'}</button><button class="btn-danger" disabled={busy} onclick={() => act('/account-api/stop')}>Stop listening</button></div>
    {:else if sessionActive}<button disabled={busy} onclick={() => act('/account-api/stop')}>Stop session</button>
    {:else if statusValue === 'not_listening' && canStart}<label class="attest"><input type="checkbox" bind:checked={soleBroadcaster} disabled={busy} /> I am the sole voice on stream. Enable speech-to-text attribution for this session.</label><p class="muted">Only check this when you are the sole voice on stream. Leave unchecked during co-streams, interviews, or voice chat.</p><button disabled={busy} onclick={startSession}>Start session</button>
    {:else}<p class="muted">Starting a session is currently unavailable. Check setup and live status.</p>{/if}
  </section>
  <section aria-labelledby="stamp-heading"><h2 id="stamp-heading">Current stamp</h2>
    {#if snapshot?.current_stamp}<p class="eyebrow">THE PROMISE</p><p class="claim">“{snapshot.current_stamp.claim}”</p><p class="muted">Receipt {snapshot.current_stamp.id}</p>
      <div class="actions"><button class="btn-stamp" disabled={busy} onclick={() => answer('admit')}>Admit</button><button class="btn-secondary" disabled={busy} onclick={() => answer('appeal')}>Appeal</button></div>
      <label for="reason">What changed? (Recorded on the ledger)</label><textarea id="reason" bind:value={reason} disabled={busy} rows="3"></textarea><button class="btn-secondary" disabled={busy} onclick={() => answer('context_changed')}>Context changed</button>
    {:else}<p class="muted">{available ? 'No active stamp on screen.' : 'Current stamp is unavailable while the runtime is inactive.'}</p>{/if}
    {#if snapshot?.current_appeal}<p>Appeal open · {snapshot.current_appeal.uphold_votes} uphold / {snapshot.current_appeal.overturn_votes} overturn · {appealRemaining}s remaining</p><div class="vote-track" role="img" aria-label={`${snapshot.current_appeal.uphold_votes} uphold votes and ${snapshot.current_appeal.overturn_votes} overturn votes`}><span style={`width:${appealUpholdPercent}%`}></span></div><div class="vote-labels"><span>Uphold {snapshot.current_appeal.uphold_votes}</span><span>Overturn {snapshot.current_appeal.overturn_votes}</span></div>{/if}
  </section>
  <section aria-labelledby="review-heading"><h2 id="review-heading">Quiet review</h2><p class="muted">Review-only mode: detected contradictions wait here privately. Approve to slam the stamp onto your stream, or Reject to dismiss.</p>
    {#each reviews as item (item.id)}<article><p class="eyebrow">THE PROMISE</p><p class="claim">“{item.claim}”</p><p class="eyebrow">HEARD NOW</p><p class="heard">“{item.reason}”</p><div class="actions"><button class="btn-stamp" disabled={busy} onclick={() => act('/api/control/review', { review_id: item.id, decision: 'approve' }, true)}>Approve</button><button class="btn-secondary" disabled={busy} onclick={() => act('/api/control/review', { review_id: item.id, decision: 'reject' }, true)}>Reject</button></div></article>{:else}<p class="muted">{available ? 'No items to review.' : 'Review is unavailable while the runtime is inactive.'}</p>{/each}
  </section>
  <section aria-labelledby="void-heading"><h2 id="void-heading">Channel Ledger &amp; Void</h2>{#if snapshot?.receipts.length}<ul class="ledger">{#each snapshot.receipts as receipt (receipt.id)}<li><div><span class="verdict">{receipt.verdict.replaceAll('_',' ')}</span><p class="claim">{receipt.claim}</p><small>{receipt.clipped_by ? `@${receipt.clipped_by}` : 'Caught by chat'} · <span title={receipt.id}>{receipt.id.slice(0,8)}</span></small></div><button class="btn-danger" disabled={busy} onclick={() => act('/api/control/void', { receipt_id: receipt.id })}>Void</button></li>{/each}</ul>{:else}<p class="muted">{available ? 'No active receipts in the channel ledger.' : 'Ledger unavailable while the runtime is inactive.'}</p>{/if}<form onsubmit={(event) => { event.preventDefault(); void act('/api/control/void', { receipt_id: voidID.trim() }); }}><label for="void-id">Receipt ID</label><input id="void-id" bind:value={voidID} required disabled={busy} /><button class="btn-danger" disabled={busy || !voidID.trim()}>Void receipt</button></form></section>
  <p class="muted">If a connection drop interrupts an action, click the same choice again; safe retries survive tab refreshes.</p>
  <p role="status" aria-live="polite">{busy ? 'Applying action…' : message}</p><p><a href={resolve("/account")}>Account setup</a></p>
</main>
<style>
:global(body){margin:0;background:var(--bg-canvas,#101713);color:var(--ink-light,#f4eedc);font:16px/1.45 system-ui,sans-serif}main{max-width:48rem;margin:auto;padding:1.25rem}header{display:flex;align-items:center;justify-content:space-between;border-bottom:1px solid var(--ink-note,#b9b3a5);padding-bottom:1rem}h1{font-size:1.7rem;margin:0}h2{font-size:1.1rem;margin:0 0 .7rem}.eyebrow{color:var(--accent-gold,#f1b969);font-size:.7rem;font-weight:800;letter-spacing:.16em;margin:0 0 .3rem}section{background:var(--bg-panel,#1a241e);border:1px solid #39453c;padding:1rem;margin:1rem 0;box-shadow:var(--shadow-hard,8px 8px 0 #080b09)}.status{display:grid;grid-template-columns:repeat(3,1fr);gap:.5rem}.status p{margin:0 0 .8rem}.status span,.status strong{display:block}.status span,.muted{color:var(--ink-muted,#d4d0c5)}.status strong{text-transform:capitalize}.claim{font-size:1.1rem;font-weight:750;overflow-wrap:anywhere}.heard{font-size:1.05rem;overflow-wrap:anywhere}button,input,textarea{font:inherit}button{background:var(--accent-gold,#f1b969);color:var(--bg-canvas,#101713);border:2px solid #080b09;padding:.55rem .8rem;font-weight:800;box-shadow:3px 3px 0 #080b09;cursor:pointer}button:hover:not(:disabled){transform:translate(-1px,-1px);box-shadow:4px 4px 0 #080b09}button:active:not(:disabled){transform:translate(2px,2px);box-shadow:1px 1px 0 #080b09}.btn-stamp{background:var(--accent-crimson,#b42e25);color:var(--ink-light,#f4eedc)}.btn-secondary{background:#16201a;color:var(--ink-light,#f4eedc);border-color:var(--ink-note,#b9b3a5)}.btn-danger{background:#2a1413;color:#f28b82;border-color:var(--accent-crimson,#b42e25)}button:disabled{opacity:.55;box-shadow:none;cursor:not-allowed}button:focus-visible,input:focus-visible,textarea:focus-visible,a:focus-visible{outline:3px solid var(--ink-light,#f4eedc);outline-offset:3px}.actions{display:flex;flex-wrap:wrap;gap:.65rem}.attest{display:flex;align-items:flex-start;gap:.6rem;margin:.8rem 0}.attest input{width:1.2rem;min-width:1.2rem;margin:.25rem 0 0}label{display:block;margin:.8rem 0 .3rem}input,textarea{box-sizing:border-box;width:100%;background:var(--bg-inset,#131b16);color:var(--ink-light,#f4eedc);border:1px solid var(--ink-note,#b9b3a5);padding:.5rem}textarea{margin-bottom:.5rem}article{border-top:1px solid #39453c;padding:.8rem 0}.ledger{list-style:none;padding:0;margin:.5rem 0}.ledger li{display:flex;justify-content:space-between;align-items:center;gap:1rem;border-top:1px solid #39453c;padding:.8rem 0}.ledger .claim{margin:.25rem 0}.ledger small{color:var(--ink-muted,#d4d0c5);overflow-wrap:anywhere}.verdict{font-size:.72rem;text-transform:uppercase;letter-spacing:.08em;color:var(--accent-gold,#f1b969)}form button{margin-top:.5rem}a{color:var(--accent-gold,#f1b969)}.vote-track{height:.75rem;background:#68251f;border:1px solid #080b09;overflow:hidden}.vote-track span{display:block;height:100%;background:var(--accent-gold,#f1b969)}.vote-labels{display:flex;justify-content:space-between;color:var(--ink-muted,#d4d0c5);font-size:.85rem}
@media(max-width:36rem){main{padding:.75rem}.status{grid-template-columns:1fr}.status p{margin:0}.ledger li{align-items:flex-start}.ledger .claim{font-size:1rem}}
</style>
