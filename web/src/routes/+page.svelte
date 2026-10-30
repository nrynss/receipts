<script lang="ts">
  import { resolve } from '$app/paths';
  import { onMount } from 'svelte';
  import Overlay from '$lib/overlay/Overlay.svelte';
  import { fixture } from '$lib/overlay/fixtures';
  import type { Display, OverlayState } from '$lib/overlay/model';
  let { data } = $props<{ data: { fixtureMode: boolean } }>();
  const stages = ['stamp', 'evidence', 'appeal', 'verdict'] as const;
  let display = $state<Display>({ ...fixture('stamp'), animate: false });
  let selected = $state<OverlayState>('stamp');
  let harness = $derived(data.fixtureMode);
  onMount(() => {
    const query = new URLSearchParams(window.location.search).get('fixture');
    if (query === 'sequence') {
      const states: OverlayState[] = ['stamp', 'evidence', 'appeal', 'verdict', 'idle'];
      let index = 0; display = fixture(states[index]);
      const timer = window.setInterval(() => { index++; display = fixture(states[index] ?? 'idle'); if (index >= states.length - 1) window.clearInterval(timer); }, 5000);
      return () => window.clearInterval(timer);
    }
    if (query && ['idle','stamp','evidence','appeal','verdict'].includes(query)) display = fixture(query as OverlayState);
  });
  function preview(state: typeof stages[number]) { selected = state; display = { ...fixture(state), animate: false }; }
</script>
<svelte:head><title>Receipts · The stream remembers</title><meta name="description" content="A broadcast ledger for promises made on stream." /></svelte:head>
{#if harness}
  <main class="harness"><Overlay {display} recordedPreview /></main>
{:else}
  <main class="landing">
    <section class="hero"><div><p class="eyebrow">LIVE STREAM ACCOUNTABILITY · BUILT WITH CODEX</p><h1>The stream remembers what you said.</h1><p class="lead">Streamers make promises on air all the time. Chat types !receipt to lock in the claim. When the streamer goes back on their word, the receipt lands on stream, and chat is the jury.</p><div class="actions"><a class="btn-primary" href={resolve('/demo/')}>Try the recorded demo <small>No login · instant replay</small></a><a class="btn-secondary" href={resolve('/account')}>Set up Receipts <small>Streamer sign-in &amp; OBS setup</small></a></div><p class="trust">Private detection. Nothing reaches the overlay until the streamer approves it.</p></div><div class="preview"><p class="eyebrow">SEE THE RECEIPT LAND</p><p class="sample-note">Recorded preview · sample timeline</p><div class="stage-tabs" role="group" aria-label="Preview overlay stage">{#each stages as stage, i (stage)}<button aria-pressed={selected === stage} onclick={() => preview(stage)}>{['Stamp','Evidence','Appeal','Verdict'][i]}</button>{/each}</div><div class="overlay-preview"><Overlay {display} recordedPreview /></div></div></section>
    <section class="steps" aria-label="How Receipts works"><article><b>01 · CHAT TAKES THE RECEIPT</b><p>!receipt clips the stream and extracts the spoken promise.</p></article><article><b>02 · THE CONTRADICTION LANDS</b><p>Live speech-to-text flags a possible walkback privately. The streamer reviews the evidence and approves the stamp before it appears on the overlay.</p></article><article><b>03 · CHAT IS THE JURY</b><p>Streamer Admits, Appeals to !uphold / !overturn, or logs Context Changed; !lore keeps the permanent score.</p></article></section>
  </main>
{/if}
<style>
  .landing{max-width:1200px;margin:auto;padding:24px clamp(18px,5vw,64px) 48px;color:#f4eedc;font-family:system-ui,sans-serif}.hero{display:grid;grid-template-columns:1fr minmax(330px,.85fr);gap:48px;align-items:center;padding:clamp(42px,8vw,100px) 0}.eyebrow{font-size:12px;letter-spacing:.16em;color:#f1b969;font-weight:900}.hero h1{font-size:clamp(42px,7vw,78px);line-height:.98;letter-spacing:-.04em;margin:20px 0}.lead{font-size:18px;line-height:1.6;color:#d4d0c5;max-width:62ch}.actions{display:flex;flex-wrap:wrap;gap:14px;margin-top:26px}.actions a{padding:14px 18px;text-decoration:none;font-weight:850;box-shadow:3px 3px 0 #080b09}.actions small{display:block;font-size:12px;margin-top:4px;font-weight:600}.btn-primary{background:#f1b969;color:#101713;border:2px solid #080b09}.btn-secondary{background:#16201a;color:#f4eedc;border:2px solid #b9b3a5}.trust{font-size:13px;color:#b9b3a5}.preview{container-type:inline-size;border:2px solid #535b50;background:#1a241e;padding:20px;box-shadow:8px 8px #080b09}.sample-note{margin:4px 0;color:#b9b3a5;font-size:12px}.stage-tabs{display:flex;flex-wrap:wrap;gap:7px;margin:14px 0}.stage-tabs button{background:#131b16;color:#f4eedc;border:1px solid #b9b3a5;padding:7px 10px;cursor:pointer}.stage-tabs button[aria-pressed=true]{background:#f1b969;color:#101713}.overlay-preview{min-height:360px;display:grid;place-items:center;padding:8px;background:#131b16;overflow:visible}.overlay-preview :global(.receipt){width:100%;max-height:none;overflow:visible}.overlay-preview :global(.receipt blockquote){font-size:clamp(16px,5cqw,27px);line-height:1.1}.overlay-preview :global(.receipt .stamp){font-size:clamp(22px,8cqw,42px)}.overlay-preview :global(.receipt .credit){font-size:clamp(11px,3.2cqw,15px)}.steps{display:grid;grid-template-columns:repeat(3,1fr);gap:14px}.steps article{background:#1a241e;border:1px solid #535b50;padding:20px;line-height:1.55}.steps b{font-size:12px;color:#f1b969;letter-spacing:.09em}.steps p{color:#d4d0c5;margin-bottom:0}.harness{min-height:100vh;display:flex;align-items:flex-end;justify-content:center;padding:3vw;box-sizing:border-box;background:transparent}@media(max-width:800px){.hero{grid-template-columns:1fr;gap:25px;padding:48px 0}.steps{grid-template-columns:1fr}}@container (max-width:600px){.overlay-preview :global(.receipt .content){display:block;padding:14px}.overlay-preview :global(.receipt .side){border-left:0;border-top:2px solid #b9b3a5;padding:14px 0 0;margin-top:10px}.overlay-preview :global(.receipt .label){margin:10px 0 5px}.overlay-preview :global(.receipt .credit){margin-top:10px}.overlay-preview :global(.receipt .fallback){padding:10px}.overlay-preview :global(.receipt .fallback p){font-size:clamp(13px,4cqw,20px)}}@media(prefers-reduced-motion:reduce){*{scroll-behavior:auto!important}}
</style>
