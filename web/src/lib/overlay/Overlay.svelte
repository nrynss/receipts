<script lang="ts">
  import { onMount } from 'svelte';
  import { clipEmbed, receiptAge, type Display } from './model';
  let { display }: { display: Display } = $props();
  let now = $state(Date.now());
  let hostname = $state('');
  let clipFailed = $state(false);
  let clipLoaded = $state(false);
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
  const verdictLabel: Record<string, string> = { admitted: 'Admitted', upheld: 'Upheld', overturned: 'Overturned', context_changed: 'Context changed', inconclusive: 'Inconclusive', undecided: 'Awaiting a decision' };
  let remaining = $derived(display.appeal ? Math.max(0, Math.ceil((Date.parse(display.appeal.closes_at) - now) / 1000)) : 0);
</script>

{#if display.state !== 'idle' && display.receipt}
  <section class:long={display.receipt.claim.length > 180 || (display.receipt.clipped_by?.length ?? 0) > 40 || display.receipt.id.length > 40} class:arrive={display.state === 'stamp' && display.animate} class="receipt" aria-label="Receipt overlay">
    <div class="topline"><span class="brand">THE STREAM REMEMBERS</span><span class="number">#{display.receipt.id}</span></div>
    <div class="content">
      <div class="copy">
        <div class="stamp">RECEIPT<span class="stamp-dot">.</span></div>
        <p class="label">THE PROMISE</p>
        <blockquote>“{display.receipt.claim}”</blockquote>
        <p class="credit">{display.receipt.clipped_by ? `Caught by @${display.receipt.clipped_by}` : 'Viewer credit unavailable'} <span>·</span> {receiptAge(display.receipt.created_at, now)}</p>
      </div>
      {#if display.state !== 'stamp'}
        <div class="side">
          {#if display.state === 'appeal' && display.appeal}
            <p class="label">CHAT IS THE JURY</p><h2>Appeal in progress</h2>
            <div class="votes"><span>UPHOLD <b>{display.appeal.uphold_votes}</b></span><span>OVERTURN <b>{display.appeal.overturn_votes}</b></span></div>
            <p class="hint">Vote in chat: !uphold or !overturn</p><p class="hint">{remaining > 0 ? `${remaining}s remaining` : 'Voting closed'}</p>
          {:else if display.state === 'verdict'}
            <p class="label">ON THE RECORD</p><h2>{verdictLabel[display.receipt.verdict] ?? 'Decision recorded'}</h2>
            <p class="hint">The verdict is in.</p>
          {:else}
            <p class="label">THE EVIDENCE</p>
            <div class="media">
              {#if url && !clipFailed}
                <iframe src={url} title="Receipt clip" allow="autoplay; fullscreen" onload={() => { clipLoaded = true; }} onerror={() => { clipFailed = true; }}></iframe>
              {/if}
              <div class="fallback"><span>{display.receipt.clip_status === 'pending' ? 'CLIP PENDING' : url && !clipFailed ? (clipLoaded ? 'CLIP FRAME LOADED · VERIFY PLAYBACK' : 'LOADING CLIP') : 'CLIP EMBED UNAVAILABLE'}</span><p>“{display.receipt.claim}”</p><small>Saved claim · the clip may omit words; transcript evidence remains authoritative.</small></div>
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
  @media(max-width:650px) { .content { padding:18px; gap:16px; }.side { padding-left:15px; }.topline,.footer { padding:8px 16px; font-size:10px; }.credit { font-size:13px; }.label { margin-top:16px; }.fallback { padding:10px; }.fallback p { font-size:13px; } }
  @media(prefers-reduced-motion:reduce) { .arrive { animation:none; } }
</style>
