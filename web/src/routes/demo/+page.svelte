<script module lang="ts">
  import { fixture } from '$lib/overlay/fixtures';
  import type { Display, OverlayState } from '$lib/overlay/model';
  export type Replay = { stage: 'ready' | 'complete'; claim?: string; verdict?: string; clip_status?: string; lore?: string; events?: string[] };
  export const emptyLore = 'Lore: 0 total | 0 open, 0 broken, 0 overturned, 0 context changed, 0 inconclusive.';
  const verdicts = ['undecided', 'admitted', 'upheld', 'overturned', 'context_changed', 'inconclusive'] as const;
  export function demoDisplay(state: OverlayState, result: Replay): Display {
    const sample = fixture(state);
    if (!sample.receipt) return sample;
    const replayVerdict = verdicts.find(verdict => verdict === result.verdict);
    return { ...sample, receipt: { ...sample.receipt, claim: 'I will never take that shortcut again.', verdict: state === 'verdict' && result.stage === 'complete' ? replayVerdict ?? sample.receipt.verdict : sample.receipt.verdict }, appeal: sample.appeal ? { ...sample.appeal, uphold_votes: 1, overturn_votes: 0 } : undefined };
  }
  export function loreReply(result: Replay) { return result.stage === 'complete' && result.lore ? result.lore : emptyLore; }
</script>

<script lang="ts">
  import Overlay from '$lib/overlay/Overlay.svelte';
  let busy = $state(false);
  let error = $state('');
  let result = $state<Replay>({ stage: 'ready' });
  let stage = $state<OverlayState>('stamp');
  let chat = $state('');
  let chatReply = $state('Try !lore to see this visitor sandbox ledger.');
  const stageNames = ['stamp', 'evidence', 'appeal', 'verdict'] as const;
  function sendChat() { chatReply = chat.trim().toLowerCase() === '!lore' ? loreReply(result) : 'This recorded chat accepts !lore.'; chat = ''; }

  async function action(name: 'replay' | 'reset') {
    if (busy) return;
    busy = true;
    error = '';
    try {
      const response = await fetch(`/demo/api/${name}`, { method: 'POST', credentials: 'same-origin' });
      if (!response.ok) throw new Error((await response.text()) || 'The demo is temporarily unavailable.');
      result = await response.json() as Replay;
      if (name === 'reset') { chat = ''; chatReply = 'Try !lore to see this visitor sandbox ledger.'; }
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
    <p class="status" aria-live="polite">{error || (result.stage === 'complete' ? 'Replay complete. Reset to try it again.' : 'Ready to replay. Visitor sandbox expires automatically.')}</p>
    <section class="overlay-stage" aria-label="Recorded overlay stages">
      <div class="stage-tabs" role="tablist" aria-label="Overlay stage">{#each stageNames as item, i (item)}<button role="tab" aria-selected={stage === item} onclick={() => stage = item}>{i + 1}. {['Stamp','Evidence','Appeal Vote','Verdict'][i]}</button>{/each}</div>
      <div class="overlay-surface"><Overlay display={demoDisplay(stage, result)} recordedPreview /></div>
    </section>
    <section class="timeline"><p class="eyebrow">BROADCAST &amp; CHAT TIMELINE</p><ol><li><b>1. Promise Captured (00:16)</b><p>@regular ran !receipt on “I will never take that shortcut again.”</p></li><li><b>2. Contradiction Caught (00:20)</b><p>Broadcaster said “Fine, I'll take the shortcut.”</p></li><li><b>3. Appeal Opened (00:20)</b><p>Broadcaster appealed the stamp to Twitch chat.</p></li><li><b>4. Jury Verdict (01:21)</b><p>Chat voted !uphold (1-0); receipt recorded as Upheld.</p></li></ol><form onsubmit={(event) => { event.preventDefault(); sendChat(); }}><label for="chat-reply">Simulated Twitch chat</label><div class="chat-line"><input id="chat-reply" bind:value={chat} placeholder="Try !lore" /><button type="submit">Send</button></div><p aria-live="polite">{chatReply}</p></form></section>
    {#if result.stage === 'complete'}
      <ol class="steps" aria-label="Recorded scenario results">
      {#each result.events ?? [] as event (event)}<li>{event}</li>{/each}
      </ol>
      <section aria-labelledby="receipt-title">
        <p class="eyebrow">THE PROMISE</p><h2 id="receipt-title">“{result.claim}”</h2>
        <p>Evidence Source: <strong>Transcript Fallback (offline demo, no Twitch clip call)</strong></p>
        <p>Final Verdict: <strong>Upheld by Chat (Promise Broken)</strong></p>
      </section>
      <section aria-labelledby="lore-title"><p class="eyebrow">CHANNEL LORE</p><h2 id="lore-title">{result.lore}</h2></section>
    {/if}
    <p class="note">Recorded scenario: The broadcaster swears “I will never take that shortcut again,” caves four seconds later, and loses the chat appeal.</p>
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
  .stage-tabs { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:16px; }.stage-tabs button { background:#16201a; color:#f4eedc; border:1px solid #b9b3a5; padding:9px 12px; }.stage-tabs button[aria-selected=true] { background:#f1b969; color:#101713; }.overlay-surface { min-height:240px; display:grid; place-items:center; padding:12px; background:#131b16; }.overlay-surface :global(.receipt) { width:100%; max-height:500px; }.timeline ol { padding-left:24px; line-height:1.5; }.timeline li { margin:14px 0; }.timeline li b { color:#f1b969; }.timeline li p { margin:4px 0; color:#d4d0c5; }.timeline label { display:block; margin-bottom:6px; font-weight:800; }.chat-line { display:flex; gap:8px; }.chat-line input { flex:1; min-width:0; padding:10px; color:#f4eedc; background:#131b16; border:1px solid #b9b3a5; }.chat-line button { border-radius:0; }.timeline form p { color:#d4d0c5; }
  .note { margin-top:28px; color:#b9b3a5; font-size:14px; line-height:1.5; }
</style>
