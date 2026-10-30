<script lang="ts">
  import { onMount } from 'svelte';
  let account = $state<{login:string} | null>(null);
  let devices = $state<{id:string; created_at:number}[]>([]);
  let code = $state('');
  let listening = $state(false);
  let statusKnown = $state(false);
  let paused = $state(false);
  let message = $state('');
  const csrf = () => decodeURIComponent(document.cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.split('=')[1] ?? '');
  async function load() {
    const response = await fetch('/auth/account');
    if (response.ok) { account = await response.json(); const d = await fetch('/auth/devices'); if (d.ok) devices = await d.json(); const state = await fetch('/account-api/status'); if (state.ok) { const value = await state.json(); listening = value.state === 'listening'; paused = value.paused; statusKnown = true; } }
  }
  async function post(path:string, body?:unknown) {
    const response = await fetch(path, { method:'POST', headers:{'X-CSRF-Token':csrf(), 'Content-Type':'application/json'}, body: body ? JSON.stringify(body) : undefined });
    if (!response.ok) throw new Error('Request could not be completed.');
  }
  async function confirm() { try { await post('/auth/pair/confirm', {code}); code=''; message='OBS source paired.'; await load(); } catch { message='Code expired or could not be confirmed.'; } }
  async function revoke(id:string) { try { await post(`/auth/devices/${encodeURIComponent(id)}/revoke`); await load(); message='OBS source revoked.'; } catch { message='Could not revoke source.'; } }
  async function stop() { try { await post('/account-api/stop'); await load(); message='Stopped listening.'; } catch { message='Could not stop listening.'; } }
  async function pause() { try { await post('/api/control/pause', {paused:!paused}); await load(); } catch { message='Could not change stamp pause.'; } }
  async function disconnect() { try { await post('/account-api/disconnect'); account=null; devices=[]; message='Twitch disconnected.'; } catch { message='Disconnect could not be confirmed. Refresh setup to check your connection.'; } }
  async function deleteAccount() {
    if (!window.confirm('Delete your account and channel data? This cannot be undone.')) return;
    try {
      const response = await fetch('/account-api/delete', {method:'POST', headers:{'X-CSRF-Token':csrf()}});
      if (!response.ok) throw new Error();
      account=null; devices=[];
      message = response.status === 202 ? 'Account deleted. Twitch revocation could not be confirmed; review your Twitch connections.' : 'Account and channel data deleted.';
    } catch { message='Deletion could not be completed. Reconnect and try again.'; }
  }
  async function logout() { try { await post('/auth/logout'); account=null; devices=[]; } catch { message='Could not sign out.'; } }
  onMount(() => { void load(); });
</script>
<svelte:head><title>Receipts setup</title></svelte:head>
<main>
  <h1>Receipts setup</h1>
  {#if account}
    <p>Connected as {account.login}</p>
    <p>{!statusKnown ? "Listening status unavailable" : listening ? (paused ? "Listening; stamps paused" : "Listening") : "Not listening"}</p>
    {#if listening}<button onclick={pause}>{paused ? "Resume stamps" : "Pause stamps"}</button><button onclick={stop}>Stop listening</button>{:else if statusKnown}<p>Listening is unavailable until a live channel adapter is ready.</p>{/if}
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
