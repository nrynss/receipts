import { decodeSnapshot, decodeReviewQueue, decodeHealth } from '$lib/api/contracts';

export type Request = typeof fetch;
export function csrf(cookie: string): string {
  const value = cookie.split('; ').find((c) => c.startsWith('receipts_csrf='))?.slice('receipts_csrf='.length) ?? '';
  try { return decodeURIComponent(value); } catch { return ''; }
}
export async function getControl(request: Request) {
  const [status, snapshot, reviews, health] = await Promise.all([
    request('/account-api/status'), request('/api/overlay'), request('/api/review'), request('/api/health')
  ]);
  if (!status.ok) throw new Error('Sign in to access your controls.');
  const state = await status.json();
  // Runtime endpoints are unavailable when no channel runtime is active.
  return { state, snapshot: snapshot.ok ? decodeSnapshot(await snapshot.json()) : null,
    reviews: reviews.ok ? decodeReviewQueue(await reviews.json()) : [], health: health.ok ? decodeHealth(await health.json()) : null,
    runtimeAvailable: snapshot.ok && reviews.ok && health.ok };
}
export function createActions(request: Request, cookie: () => string, key: () => string = () => crypto.randomUUID()) {
  let pending = false;
  let retry: { signature: string; actionKey: string } | undefined;
  return {
    async send(path: string, body: Record<string, unknown> = {}, keyed = false) {
      if (pending) return false;
      const signature = JSON.stringify([path, body]);
      if (keyed && retry && retry.signature !== signature) throw new Error('Retry the pending action before choosing another.');
      if (keyed && !retry) retry = { signature, actionKey: key() };
      pending = true;
      try {
        const response = await request(path, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf(cookie()) }, body: JSON.stringify(keyed ? { ...body, action_key: retry!.actionKey } : body) });
        if (!response.ok) throw new Error('Action could not be applied. Retry or refresh to check the current state.');
        retry = undefined;
        return true;
      } finally { pending = false; }
    }
  };
}
