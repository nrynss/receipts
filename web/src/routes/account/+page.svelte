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
  const csrf = () => decodeURIComponent(document.cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.split('=')[1] ?? '');
  async function load() {
    const version=++statusVersion; poll?.cancel();
    const response = await request('/auth/account');
    if (response.ok) { const nextAccount = await response.json(); if (disposed || version!==statusVersion) return; account = nextAccount; const d = await request('/auth/devices'); if (d.ok) {const nextDevices=await d.json(); if (disposed || version!==statusVersion) return; devices=nextDevices;} const statusResponse = await request('/account-api/status'); if (statusResponse.ok) { const value = await statusResponse.json(); if (disposed || version!==statusVersion) return; if (['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(value.state)) statusValue = value.state; canStart = value.can_start === true; transcriptionEnabled = value.transcription_enabled === true; uncertainSession=false; } }
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
  onMount(() => { if (import.meta.env.DEV) { const fixture = new URLSearchParams(window.location.search).get('fixture'); if (fixture && ['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(fixture)) request = fixtureFetch(fixture); } poll=statusRefresh(request, (value) => { if (typeof value.state === 'string' && ['unavailable','unknown','offline','missing_grant','capacity','not_listening','session_active','listening','paused'].includes(value.state)) statusValue=value.state as State; canStart=value.can_start===true;transcriptionEnabled=value.transcription_enabled===true;uncertainSession=false; }, () => { uncertainSession=sessionActive;statusValue='unknown';transcriptionEnabled=false; }); void load(); const timer=window.setInterval(() => { if (account) void refreshStatus(); },5000); return () => { disposed=true; statusVersion++; poll?.dispose(); window.clearInterval(timer); }; });
</script>
<svelte:head><title>Receipts setup</title></svelte:head>
<main>
  <h1>Receipts setup</h1>
  {#if account}
    <p>Connected as {account.login}</p>
    <p><a href={resolve("/control")}>Open streamer control dock</a></p>
    <p>{({unavailable:'Listening is unavailable.',unknown:'Stream status is unknown. Try again.',offline:'Your channel is offline.',missing_grant:'Reconnect Twitch to grant the required permissions.',capacity:'Listening capacity is full. Try again later.',not_listening:'Your channel is live and not listening.',session_active:'Live session active. Audio capture is not attached.',listening:'Listening.',paused:'Listening; stamps paused.'})[statusValue]}</p>
    {#if listening}<p>{transcriptionEnabled ? 'Transcription enabled for your sole voice.' : 'Audio is being monitored; transcription is off because sole-speaker confirmation was not given.'}</p><button onclick={pause}>{paused ? "Resume stamps" : "Pause stamps"}</button><button onclick={stop}>Stop listening</button>{:else if sessionActive}<button onclick={stop}>Stop session</button>{:else if statusValue === "not_listening"}{#if canStart}<label><input type="checkbox" bind:checked={soleBroadcaster} /> I am the only person speaking in this broadcast. Enable transcript attribution for this session.</label><p>Leave this unchecked if other voices may be heard. Microphone capture is unavailable.</p><button onclick={start}>Start session</button>{:else}<p>Starting a session is currently unavailable.</p>{/if}{:else if statusValue === "missing_grant"}<button onclick={() => { window.location.href = "/auth/twitch"; }}>Reconnect Twitch</button>{/if}
    <h2>OBS browser source</h2>
    <p>Add this URL as a browser source, then enter the code shown there.</p>
    <code>{typeof window === 'undefined' ? '/obs' : `${window.location.origin}/obs`}</code>
    <form onsubmit={(event) => { event.preventDefault(); void confirm(); }}><label for="pair-code">Pairing code</label><input id="pair-code" bind:value={code} autocomplete="off" required /><button>Confirm source</button></form>
    <h3>Paired sources</h3>
    {#if devices.length === 0}<p>No paired sources yet.</p>{/if}
    {#each devices as device (device.id)}<p>Source added {new Date(device.created_at).toLocaleDateString()} <button onclick={() => revoke(device.id)}>Revoke</button></p>{/each}
    <button onclick={logout}>Sign out</button><button onclick={disconnect}>Disconnect Twitch</button><button onclick={deleteAccount}>Delete account</button>
  {:else}<p>Connect your Twitch account to set up Receipts.</p><button onclick={() => { window.location.href = "/auth/twitch"; }}>Connect Twitch</button>{/if}
  <p role="status">{message}</p>
</main>
<style>main{font:1rem system-ui;max-width:42rem;margin:3rem auto;padding:1rem}button,input{font:inherit;padding:.5rem;margin:.5rem}code{overflow-wrap:anywhere}</style>
