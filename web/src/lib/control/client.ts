import { decodeSnapshot, decodeReviewQueue, decodeHealth } from '$lib/api/contracts';

export type Request = typeof fetch;
export function csrf(cookie: string): string {
  const value = cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.slice('receipts_csrf='.length) ?? '';
  try { return decodeURIComponent(value); } catch { return ''; }
}
export async function getControl(request: Request) {
  const [account, status, snapshot, reviews, health] = await Promise.all([
    request('/auth/account'),
    request('/account-api/status'), request('/api/overlay'), request('/api/review'), request('/api/health')
  ]);
  if (!account.ok || !status.ok) throw new Error('Sign in to access your controls.');
  const owner = await account.json();
  if (typeof owner.id !== 'string' || typeof owner.channel_id !== 'string') throw new Error('Account unavailable.');
  const state = await status.json();
  // Runtime endpoints are unavailable when no channel runtime is active.
  return { owner, state, snapshot: snapshot.ok ? decodeSnapshot(await snapshot.json()) : null,
    reviews: reviews.ok ? decodeReviewQueue(await reviews.json()) : [], health: health.ok ? decodeHealth(await health.json()) : null,
    runtimeAvailable: snapshot.ok && reviews.ok && health.ok };
}
export function createActions(request: Request, cookie: () => string, key: () => string = () => crypto.randomUUID(), storage?: Storage) {
  let pending = false;
  let retry: { signature: string; actionKey: string } | undefined;
  let scope = '';
  const storageKey = () => `receipts-control-retry:${scope}`;
  function save() { if (!storage || !scope) return; if (retry) storage.setItem(storageKey(), JSON.stringify(retry)); else storage.removeItem(storageKey()); }
  return {
    setScope(owner: string, channel: string) {
      scope = JSON.stringify([owner, channel]); retry = undefined;
      try { const raw = storage?.getItem(storageKey()); if (raw) { const item = JSON.parse(raw); if (typeof item.signature === 'string' && typeof item.actionKey === 'string') retry = item; } } catch { throw new Error('Retry storage is unavailable.'); }
    },
    async send(path: string, body: Record<string, unknown> = {}, keyed = false) {
      if (pending) return false;
      // Only action identifiers are persisted; context notes and private review text are not.
      const signature = JSON.stringify([path, body.receipt_id, body.review_id, body.receipt_decision, body.decision]);
      if (keyed && retry && retry.signature !== signature) throw new Error('Retry the pending action before choosing another.');
      if (keyed && !scope && storage) throw new Error('Account identity unavailable. Refresh and try again.');
      if (keyed && !retry) { retry = { signature, actionKey: key() }; save(); }
      pending = true;
      try {
        const response = await request(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf(cookie()) }, body: JSON.stringify(keyed ? { ...body, action_key: retry!.actionKey } : body) });
        if (!response.ok) throw new Error('Action could not be applied. Retry or refresh to check the current state.');
        if (keyed) { retry = undefined; save(); }
        return true;
      } finally { pending = false; }
    }
  };
}
