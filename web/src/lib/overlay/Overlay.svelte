<script lang="ts">
  import { onMount } from 'svelte';
  import { clipEmbed, receiptAge, type Display } from './model';
  let { display, recordedPreview = false }: { display: Display; recordedPreview?: boolean } = $props();
  let now = $state(Date.now());
  let hostname = $state('');
  let clipFailed = $state(false);
  let clipLoaded = $state(false);
  let revealEvidence = $state(false);
  $effect(() => {
    revealEvidence = false;
    if (display.state === 'stamp' && display.animate) {
      const timer = window.setTimeout(() => { revealEvidence = true; }, 1500);
      return () => window.clearTimeout(timer);
    }
  });
  let url = $derived(display.receipt?.clip_status === 'ready' ? clipEmbed(display.receipt.clip_url, hostname) : undefined);
  onMount(() => {
    hostname = window.location.hostname;
    const clock = window.setInterval(() => { now = Date.now(); }, 1000);
    return () => window.clearInterval(clock);
  });
  $effect(() => {
    const currentURL = url; clipFailed = false; clipLoaded = false;
    if (currentURL) { const timer = setTimeout(() => { if (!clipLoaded) clipFailed = true; }, 8000); return () => clearTimeout(timer); }
  });
  const verdictLabel: Record<string, string> = { admitted: 'Promise Broken (Admitted)', upheld: 'Upheld by Chat', overturned: 'Overturned by Chat', context_changed: 'Context Changed', inconclusive: 'Inconclusive', undecided: 'Awaiting Decision' };
  let remaining = $derived(display.appeal ? Math.max(0, Math.ceil((Date.parse(display.appeal.closes_at) - (recordedPreview ? Date.parse(display.receipt?.created_at ?? '') : now)) / 1000)) : 0);
  function formatDuration(seconds: number) { const minutes = Math.floor(seconds / 60); return minutes ? `${minutes}m ${seconds % 60}s` : `${seconds}s`; }
</script>

{#if display.state !== 'idle' && display.receipt}
  <section class:long={display.receipt.claim.length > 180 || (display.receipt.clipped_by?.length ?? 0) > 40 || display.receipt.id.length > 40} class:arrive={display.state === 'stamp' && display.animate} class="receipt" aria-label="Receipt overlay">
    <div class="topline"><span class="brand">THE STREAM REMEMBERS</span><span class="number" title={display.receipt.id} aria-label={`Receipt ${display.receipt.id}`}>RECEIPT #{display.receipt.id.slice(0, 8)}<span class="sr-only"> {display.receipt.id}</span></span></div>
    <div class="content">
      <div class="copy">
        <div class="stamp">RECEIPT<span class="stamp-dot">.</span></div>
        <p class="label">THE PROMISE</p>
        <blockquote>“{display.receipt.claim}”</blockquote>
        <p class="credit">{display.receipt.clipped_by ? `Caught by @${display.receipt.clipped_by}` : 'Caught by chat'} <span>·</span> {recordedPreview ? `Recorded preview · sample timestamp ${display.receipt.created_at.replace('T', ' ').replace(/(?:\.\d+)?Z$/, ' UTC')}` : receiptAge(display.receipt.created_at, now)}</p>
      </div>
      {#if display.state !== 'stamp' || revealEvidence}
        <div class="side">
          {#if display.state === 'appeal' && display.appeal}
            <p class="label">CHAT IS THE JURY</p><h2>Appeal in progress</h2>
            <div class="vote-bar" role="img" aria-label={`${display.appeal.uphold_votes} uphold votes and ${display.appeal.overturn_votes} overturn votes`}><span style={`width:${(display.appeal.uphold_votes + display.appeal.overturn_votes) ? display.appeal.uphold_votes * 100 / (display.appeal.uphold_votes + display.appeal.overturn_votes) : 50}%`}></span></div><div class="votes"><span>UPHOLD <b>{display.appeal.uphold_votes}</b></span><span>OVERTURN <b>{display.appeal.overturn_votes}</b></span></div>
            <p class="hint">Type !uphold or !overturn in chat</p><p class="hint">{recordedPreview ? `Recorded vote · ${formatDuration(remaining)} remaining at stage start` : remaining > 0 ? `${remaining}s remaining` : 'Voting closed'}</p>
          {:else if display.state === 'verdict'}
            <p class="label">ON THE RECORD</p><h2>{verdictLabel[display.receipt.verdict] ?? 'Decision recorded'}</h2>
            <p class="hint">The verdict is on the record.</p>
          {:else}
            <p class="label">THE EVIDENCE</p>
            <div class="media">
              {#if url && !clipFailed}
                <iframe src={url} title="Receipt clip" allow="autoplay; fullscreen" onload={() => { clipLoaded = true; }} onerror={() => { clipFailed = true; }}></iframe>
              {/if}
              <div class="fallback"><span>{display.receipt.clip_status === 'pending' ? 'CLIP PENDING · TRANSCRIPT ON FILE' : url && !clipFailed ? (clipLoaded ? 'CLIP FRAME LOADED · VERIFY PLAYBACK' : 'LOADING TWITCH CLIP…') : 'CLIP EMBED UNAVAILABLE · TRANSCRIPT ON FILE'}</span>{#if !(url && !clipFailed)}<p>“{display.receipt.claim}”</p>{/if}<small>Saved claim from live transcript · transcript evidence remains authoritative if clip timing shifts.</small></div>
            </div>
          {/if}
        </div>
      {/if}
    </div>
    <div class="footer"><span>KEEP THE RECEIPTS</span><span>✳</span></div>
  </section>
{/if}

<style>
  .receipt { box-sizing:border-box; width:min(94vw, 1120px); color:#171d1a; background:#f4eedc; border:3px solid #171d1a; box-shadow:8px 8px 0 #171d1a; font-family:system-ui,sans-serif; overflow:auto; max-height:85vh; }
  .arrive { animation:land .5s cubic-bezier(.16,1.4,.3,1) both; }
  @keyframes land { from { opacity:0; transform:scale(1.15) rotate(-3deg); } to { opacity:1; transform:scale(1) rotate(0); } }
  .topline,.footer { display:flex; gap:12px; min-width:0; justify-content:space-between; align-items:center; background:#171d1a; color:#f4eedc; padding:10px 24px; font-size:12px; font-weight:800; letter-spacing:.18em; }
  .number { overflow-wrap:anywhere; min-width:0; text-align:right; }
  .content { display:flex; gap:28px; padding:26px 28px 30px; }.copy { flex:1; min-width:0; }.side { flex:0 0 40%; min-width:0; border-left:2px solid #b9b3a5; padding-left:26px; }
  .stamp { display:inline-block; color:#b42e25; border:5px solid #b42e25; padding:0 12px; font-size:clamp(30px,5vw,64px); font-weight:1000; letter-spacing:.06em; line-height:1.15; transform:rotate(-3deg); }.stamp-dot { color:#171d1a; }
  .label { margin:24px 0 8px; font-size:12px; font-weight:900; letter-spacing:.17em; color:#535b50; }.side .label { margin-top:0; }
  blockquote { margin:0; font-size:clamp(23px,3vw,40px); font-weight:800; line-height:1.12; overflow-wrap:anywhere; }.credit { overflow-wrap:anywhere; margin:20px 0 0; font-size:16px; font-weight:700; }.credit span { color:#b42e25; padding:0 5px; }
  .media { position:relative; background:#242c27; color:#f4eedc; } iframe { width:100%; aspect-ratio:16/9; border:0; display:block; }.fallback { display:flex; flex-direction:column; justify-content:center; padding:18px; background:#242c27; box-sizing:border-box; }.fallback span { color:#f1b969; font-size:11px; font-weight:900; letter-spacing:.16em; }.fallback p { margin:10px 0; font-size:clamp(15px,1.5vw,22px); line-height:1.2; overflow-wrap:anywhere; }.fallback small,.hint { font-size:13px; }.votes { display:flex; gap:24px; font-size:12px; font-weight:900; letter-spacing:.1em; }.votes b { display:block; font-size:36px; letter-spacing:0; } h2 { font-size:clamp(23px,2.4vw,34px); line-height:1.1; margin:10px 0 20px; }
  .long blockquote { font-size:clamp(15px,2vw,26px); }
  .long .fallback p { font-size:clamp(13px,1.3vw,17px); }
  .footer { padding:8px 24px; color:#f1b969; }
  .vote-bar { height:14px; margin:16px 0 10px; background:#b42e25; border:2px solid #171d1a; }.vote-bar span { display:block; height:100%; max-width:100%; background:#f1b969; border-right:2px solid #171d1a; }
  .sr-only { position:absolute; width:1px; height:1px; padding:0; margin:-1px; overflow:hidden; clip:rect(0,0,0,0); white-space:nowrap; border:0; }
  @media(max-width:650px) { .content { padding:18px; gap:16px; }.side { padding-left:15px; }.topline,.footer { padding:8px 16px; font-size:10px; }.credit { font-size:13px; }.label { margin-top:16px; }.fallback { padding:10px; }.fallback p { font-size:13px; } }
  @media(prefers-reduced-motion:reduce) { .arrive { animation:none; } }
</style>
