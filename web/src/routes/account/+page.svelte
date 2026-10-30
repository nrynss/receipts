<script lang="ts">
  import { onMount } from 'svelte';
  import { statusRefresh } from '$lib/account/status-refresh';
  import { resolve } from '$app/paths';
  import { fixtureFetch } from '$lib/account/testing/fixture';
  type State = 'unavailable' | 'unknown' | 'offline' | 'missing_grant' | 'capacity' | 'not_listening' | 'session_active' | 'listening' | 'paused';
  let request: typeof fetch = fetch;
  let statusValue = $state<State>('unavailable');
  let canStart = $state(false);
  let soleBroadcaster = $state(false);
  let transcriptionEnabled = $state(false);
  let uncertainSession = $state(false);
  let poll: ReturnType<typeof statusRefresh> | undefined;
  let statusVersion = 0;
  let disposed = false;
  let statusActionPending = false;
  let account = $state<{login:string} | null>(null);
  let devices = $state<{id:string; created_at:number}[]>([]);
  let code = $state('');
  let listening = $derived(statusValue === 'listening' || statusValue === 'paused');
  let sessionActive = $derived(listening || statusValue === 'session_active' || uncertainSession);
  let paused = $derived(statusValue === 'paused');
  let message = $state('');
  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let obsURL = $derived(typeof window === 'undefined' ? '/obs' : `${window.location.origin}/obs`);
  const csrf = () => decodeURIComponent(document.cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.split('=')[1] ?? '');
  function statusUnknown() { uncertainSession=sessionActive;statusValue='unknown';transcriptionEnabled=false; }
  async function load() {
    const version=++statusVersion; poll?.cancel();
    try {
      const response = await request('/auth/account'); if (!response.ok) throw new Error('Account unavailable');
      const nextAccount = await response.json(); if (disposed || version!==statusVersion) return; account=nextAccount;
      const d=await request('/auth/devices'); if (d.ok) { const nextDevices=await d.json(); if (disposed || version!==statusVersion) return; devices=nextDevices; }
      const statusResponse=await request('/account-api/status'); if (!statusResponse.ok) throw new Error('Status unavailable');
      const value=await statusResponse.json(); if (disposed || version!==statusVersion) return;
      if (['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(value.state)) statusValue=value.state;
      canStart=value.can_start===true;transcriptionEnabled=value.transcription_enabled===true;uncertainSession=false;
    } catch { if (!disposed && version===statusVersion) statusUnknown(); }
  }
  async function post(path:string, body?:unknown) {
    const response = await request(path, { method:'POST', headers:{'X-CSRF-Token':csrf(), 'Content-Type':'application/json'}, body: body ? JSON.stringify(body) : undefined });
    if (!response.ok) throw new Error('Request could not be completed.');
  }
  async function confirm() { try { await post('/auth/pair/confirm', {code}); code=''; message='OBS source paired.'; await load(); } catch { message='Code expired or could not be confirmed.'; } }
  async function revoke(id:string) { try { await post(`/auth/devices/${encodeURIComponent(id)}/revoke`); await load(); message='OBS source revoked.'; } catch { message='Could not revoke source.'; } }
  function refreshStatus() { if (!statusActionPending) { statusVersion++; void poll?.run(); } }
  async function start() { poll?.cancel(); statusVersion++; statusActionPending=true; const attested = soleBroadcaster; soleBroadcaster=false; try { await post('/account-api/start', {sole_broadcaster: attested}); await load(); message='Live session started.'; } catch { await load(); message='Listening could not start. Check the status and try again.'; } finally { statusActionPending=false; } }
  async function stop() { poll?.cancel(); statusVersion++; statusActionPending=true; soleBroadcaster=false; try { await post('/account-api/stop'); await load(); uncertainSession=false; message='Stopped listening.'; } catch { message='Could not stop listening.'; } finally { statusActionPending=false; } }
  async function pause() { try { await post('/api/control/pause', {paused:!paused}); await load(); } catch { message='Could not change stamp pause.'; } }
  async function disconnect() { try { await post('/account-api/disconnect'); account=null; devices=[]; message='Twitch disconnected.'; } catch { message='Disconnect could not be confirmed. Refresh setup to check your connection.'; } }
  async function deleteAccount() {
    if (!window.confirm('Delete your account and channel data? This cannot be undone.')) return;
    try {
      const response = await request('/account-api/delete', {method:'POST', headers:{'X-CSRF-Token':csrf()}});
      if (!response.ok) throw new Error();
      account=null; devices=[];
      message = response.status === 202 ? 'Account deleted. Twitch revocation could not be confirmed; review your Twitch connections.' : 'Account and channel data deleted.';
    } catch { message='Deletion could not be completed. Reconnect and try again.'; }
  }
  async function logout() { try { await post('/auth/logout'); account=null; devices=[]; } catch { message='Could not sign out.'; } }
  async function copyURL() { try { await navigator.clipboard.writeText(obsURL); copied=true; clearTimeout(copyTimer); copyTimer=setTimeout(() => copied=false,1800); } catch { message='Copy failed. Select and copy the OBS URL manually.'; } }
  onMount(() => { if (import.meta.env.DEV) { const fixture = new URLSearchParams(window.location.search).get('fixture'); if (fixture && ['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(fixture)) request = fixtureFetch(fixture); } poll=statusRefresh(request, (value) => { if (typeof value.state === 'string' && ['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(value.state)) statusValue=value.state as State; canStart=value.can_start===true;transcriptionEnabled=value.transcription_enabled===true;uncertainSession=false; }, statusUnknown); void load(); const timer=window.setInterval(() => { if (account) void refreshStatus(); },5000); return () => { disposed=true; statusVersion++; poll?.dispose(); window.clearInterval(timer); }; });
</script>
<svelte:head><title>Receipts setup</title></svelte:head>
<main>
  <p class="eyebrow">BROADCAST SETUP</p><h1>Receipts setup</h1>
  {#if account}
    <p>Connected as <strong>@{account.login}</strong> · <a href={resolve("/control")}>Open streamer control dock</a></p>
    <section aria-labelledby="broadcast-heading"><h2 id="broadcast-heading">Broadcast &amp; Listening Status</h2>
      <p class="status status-{statusValue}">{({unavailable:'Live listening is not configured on this server.',unknown:'Could not verify stream status. Retrying automatically…',offline:'Offline · Go live on Twitch to start a Receipts session.',missing_grant:'Missing Twitch permissions · Reconnect to grant chat and clip access.',capacity:'Server capacity reached · All active channel slots are currently in use.',not_listening:'Channel is LIVE · Ready to start listening.',session_active:'Live session active · Waiting for broadcast audio stream.',listening:'Listening · Manual review enabled.',paused:'Listening · Overlay stamps paused.'})[statusValue]}</p>
      {#if listening}<p>{transcriptionEnabled ? 'Solo broadcast confirmed · Speech-to-text attribution is active.' : 'Speech-to-text is paused for this session because sole-speaker confirmation was not checked.'}</p><button class="btn-secondary" onclick={pause}>{paused ? "Resume stamps" : "Pause stamps"}</button> <button class="btn-danger" onclick={stop}>Stop listening</button>{:else if sessionActive}<button class="btn-danger" onclick={stop}>Stop session</button>{:else if statusValue === "not_listening"}{#if canStart}<label class="attest"><input type="checkbox" bind:checked={soleBroadcaster} /> I am the only person speaking in this broadcast. Enable transcript attribution for this session.</label><p class="muted">Only check this when you are the sole voice on stream. Leave unchecked during co-streams, interviews, or voice chat.</p><button onclick={start}>Start session</button>{:else}<p>Starting a session is currently unavailable.</p>{/if}{:else if statusValue === "missing_grant"}<button onclick={() => { window.location.href = "/auth/twitch"; }}>Reconnect Twitch</button>{/if}
    </section>
    <section aria-labelledby="obs-heading"><h2 id="obs-heading">OBS Browser Source Pairing</h2><p>Add this URL as an OBS Browser Source (1920 × 1080), then enter the pairing code displayed in OBS.</p><div class="copy-row"><code>{obsURL}</code><button class="btn-secondary" onclick={copyURL}>{copied ? 'Copied!' : 'Copy URL'}</button></div>
      <form onsubmit={(event) => { event.preventDefault(); void confirm(); }}><label for="pair-code">Pairing code</label><input id="pair-code" bind:value={code} autocomplete="off" required /><button>Confirm source</button></form>
      <h3>Paired sources</h3>
      {#if devices.length === 0}<p class="muted">No paired sources yet.</p>{/if}
      {#each devices as device (device.id)}<div class="paired"><p>Source #{device.id.slice(0, 6)} · added {new Date(device.created_at).toLocaleString()}</p><button class="btn-secondary" onclick={() => revoke(device.id)}>Revoke</button></div>{/each}
    </section>
    <section aria-labelledby="danger-heading"><h2 id="danger-heading">Account &amp; Danger Zone</h2><div class="session-actions"><p><strong>Session</strong></p><button class="btn-secondary" onclick={logout}>Sign out</button></div><div class="danger"><p><strong>Danger Zone</strong></p><p class="muted">Disconnecting or deleting ends account access. Deletion removes channel data.</p><button class="btn-danger" onclick={disconnect}>Disconnect Twitch</button> <button class="btn-danger" onclick={deleteAccount}>Delete account</button></div></section>
  {:else}<section><p>Sign in with your Twitch broadcaster account to pair OBS and manage live sessions.</p><button onclick={() => { window.location.href = "/auth/twitch"; }}>Connect Twitch</button></section>{/if}
  <p role="status">{message}</p>
</main>
<style>:global(body){margin:0;background:var(--bg-canvas,#101713);color:var(--ink-light,#f4eedc);font:1rem/1.5 system-ui,sans-serif}main{max-width:48rem;margin:2rem auto;padding:1rem}.eyebrow{color:var(--accent-gold,#f1b969);font-size:.72rem;letter-spacing:.16em;font-weight:800;margin:0}h1{margin:.15rem 0 1rem}h2{font-size:1.15rem;margin:0 0 .8rem}h3{font-size:1rem;margin:1.2rem 0 .5rem}section{background:var(--bg-panel,#1a241e);border:1px solid #39453c;padding:1.1rem;margin:1rem 0;box-shadow:var(--shadow-hard,8px 8px 0 #080b09)}button,input{font:inherit;padding:.55rem .8rem}button{background:var(--accent-gold,#f1b969);color:var(--bg-canvas,#101713);border:2px solid #080b09;font-weight:800;box-shadow:3px 3px 0 #080b09;cursor:pointer}button:hover{transform:translate(-1px,-1px);box-shadow:4px 4px 0 #080b09}button:focus-visible,input:focus-visible,a:focus-visible{outline:3px solid var(--ink-light,#f4eedc);outline-offset:3px}.btn-secondary{background:#16201a;color:var(--ink-light,#f4eedc);border-color:var(--ink-note,#b9b3a5)}.btn-danger{background:#2a1413;color:#f28b82;border-color:var(--accent-crimson,#b42e25)}a{color:var(--accent-gold,#f1b969)}.status{padding:.8rem;background:var(--bg-inset,#131b16);border-left:4px solid var(--accent-gold,#f1b969)}.status-offline,.status-unknown,.status-unavailable{border-left-color:var(--ink-note,#b9b3a5)}.muted{color:var(--ink-muted,#d4d0c5)}.attest{display:flex;align-items:flex-start;gap:.6rem}.attest input{width:1.2rem;min-width:1.2rem;margin:.25rem 0 0}.copy-row{display:flex;align-items:center;gap:.75rem}.copy-row code{flex:1;overflow-wrap:anywhere;background:var(--bg-inset,#131b16);padding:.7rem}.paired,.session-actions{display:flex;justify-content:space-between;align-items:center;gap:1rem;border-top:1px solid #39453c;padding:.6rem 0}.danger{border:1px solid var(--accent-crimson,#b42e25);background:#211514;padding:1rem;margin-top:1rem}.danger p:first-child{color:#f28b82}form{max-width:24rem}label{display:block;margin:.8rem 0 .3rem}input{box-sizing:border-box;background:var(--bg-inset,#131b16);color:var(--ink-light,#f4eedc);border:1px solid var(--ink-note,#b9b3a5)}@media(max-width:36rem){main{margin:1rem auto}.copy-row{align-items:stretch;flex-direction:column}.paired{align-items:flex-start}}
</style>
