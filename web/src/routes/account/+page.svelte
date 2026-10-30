<script lang="ts">
  import { onMount } from 'svelte';
  import { fixtureFetch } from '$lib/account/testing/fixture';
  type State = 'unavailable' | 'unknown' | 'offline' | 'missing_grant' | 'capacity' | 'not_listening' | 'listening' | 'paused';
  let request: typeof fetch = fetch;
  let statusValue = $state<State>('unavailable');
  let canStart = $state(false);
  let account = $state<{login:string} | null>(null);
  let devices = $state<{id:string; created_at:number}[]>([]);
  let code = $state('');
  let listening = $derived(statusValue === 'listening' || statusValue === 'paused');
  let paused = $derived(statusValue === 'paused');
  let message = $state('');
  const csrf = () => decodeURIComponent(document.cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.split('=')[1] ?? '');
  async function load() {
    const response = await request('/auth/account');
    if (response.ok) { account = await response.json(); const d = await request('/auth/devices'); if (d.ok) devices = await d.json(); const statusResponse = await request('/account-api/status'); if (statusResponse.ok) { const value = await statusResponse.json(); if (['unavailable','unknown','offline','missing_grant','capacity','not_listening','listening','paused'].includes(value.state)) statusValue = value.state; canStart = value.can_start === true; } }
  }
  async function post(path:string, body?:unknown) {
    const response = await request(path, { method:'POST', headers:{'X-CSRF-Token':csrf(), 'Content-Type':'application/json'}, body: body ? JSON.stringify(body) : undefined });
    if (!response.ok) throw new Error('Request could not be completed.');
  }
  async function confirm() { try { await post('/auth/pair/confirm', {code}); code=''; message='OBS source paired.'; await load(); } catch { message='Code expired or could not be confirmed.'; } }
  async function revoke(id:string) { try { await post(`/auth/devices/${encodeURIComponent(id)}/revoke`); await load(); message='OBS source revoked.'; } catch { message='Could not revoke source.'; } }
  async function start() { try { await post('/account-api/start'); await load(); message='Listening started.'; } catch { await load(); message='Listening could not start. Check the status and try again.'; } }
  async function stop() { try { await post('/account-api/stop'); await load(); message='Stopped listening.'; } catch { message='Could not stop listening.'; } }
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
  onMount(() => { if (import.meta.env.DEV) { const fixture = new URLSearchParams(window.location.search).get('fixture'); if (fixture && ['unavailable','unknown','offline','missing_grant','capacity','not_listening','listening','paused'].includes(fixture)) request = fixtureFetch(fixture); } void load(); });
</script>
<svelte:head><title>Receipts setup</title></svelte:head>
<main>
  <h1>Receipts setup</h1>
  {#if account}
    <p>Connected as {account.login}</p>
    <p>{({unavailable:'Listening is unavailable.',unknown:'Stream status is unknown. Try again.',offline:'Your channel is offline.',missing_grant:'Reconnect Twitch to grant the required permissions.',capacity:'Listening capacity is full. Try again later.',not_listening:'Your channel is live and not listening.',listening:'Listening.',paused:'Listening; stamps paused.'})[statusValue]}</p>
    {#if listening}<button onclick={pause}>{paused ? "Resume stamps" : "Pause stamps"}</button><button onclick={stop}>Stop listening</button>{:else if statusValue === "not_listening"}{#if canStart}<button onclick={start}>Start listening</button>{:else}<p>Starting listening is currently unavailable.</p>{/if}{:else if statusValue === "missing_grant"}<button onclick={() => { window.location.href = "/auth/twitch"; }}>Reconnect Twitch</button>{/if}
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
