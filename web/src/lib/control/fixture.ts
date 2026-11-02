// Development-only rehearsal data; no network or provider access.
export function fixtureFetch(reviewDelay = 0): typeof fetch {
  let reviewReads = 0;
  let paused = false;
  let stamp = true;
  let review = true;
  return async (input, init) => {
    const path = String(input);
    const ok = (value: unknown) => Response.json(value);
    if (path === '/auth/account') return ok({ id: 'fixture-owner', channel_id: 'fixture-channel' });
    if (path === '/account-api/status') return ok({ state: paused ? 'paused' : 'listening', can_start: false });
    if (path === '/api/health') return ok({ status: 'ok', mode: 'fixture', live_confirmed: true });
    if (path === '/api/overlay') return ok({ epoch: 'fixture', channel: 'fixture', live: true, sequence: 0, receipts: [], ...(stamp ? { current_stamp: { id: 'fixture-receipt', claim: 'I will finish the puzzle without a hint.', clip_status: 'pending', verdict: 'undecided', created_at: '2025-01-01T00:00:00Z' } } : {}) });
    if (path === '/api/review') return ok(review && ++reviewReads > reviewDelay ? [{ id: 'fixture-review', claim: 'I will try the blue route.', reason: 'Possible contradiction needs review.', created_at: '2025-01-01T00:00:00Z' }] : []);
    if (path === '/api/control/pause') paused = JSON.parse(String(init?.body)).paused;
    if (path === '/api/control/decision') stamp = false;
    if (path === '/api/control/review') review = false;
    return new Response(null, { status: 204 });
  };
}
