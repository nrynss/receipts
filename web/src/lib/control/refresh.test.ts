import { describe, expect, it, vi } from 'vitest';
import { controlRefresh } from './refresh';

function fixture() {
  let reviews: unknown[] = [];
  const request = vi.fn(async (input: RequestInfo | URL) => {
    const path = String(input);
    if (path === '/auth/account') return Response.json({ id: 'owner', channel_id: 'channel' });
    if (path === '/account-api/status') return Response.json({ state: 'listening', audio_capture_active: true });
    if (path === '/api/overlay') return Response.json({ epoch: 'e', channel: 'channel', live: true, receipts: [], sequence: 0 });
    if (path === '/api/review') return Response.json(reviews);
    return Response.json({ status: 'ok', mode: 'fixture', live_confirmed: true });
  });
  return { request, setReviews(value: unknown[]) { reviews = value; } };
}

describe('complete control polling', () => {
  it('receives new reviews and stable authoritative order across polls', async () => {
    const f = fixture();
    const receive = vi.fn();
    const poll = controlRefresh(f.request as typeof fetch, receive, error => { throw error; });
    await poll.run();
    expect(receive.mock.calls[0][0].reviews).toEqual([]);
    const items = [
      { id: 'new', claim: 'promise', reason: 'later line', created_at: '2026-01-01T00:00:02Z' },
      { id: 'old', claim: 'earlier promise', reason: 'other line', created_at: '2026-01-01T00:00:01Z' }
    ];
    f.setReviews(items);
    await poll.run();
    await poll.run();
    expect(receive.mock.calls.slice(1).map(call => call[0].reviews)).toEqual([items, items]);
    poll.dispose();
  });

  it('does not overlap polls or deliver a cancelled read over an action', async () => {
    const f = fixture();
    let finish!: () => void;
    const gate = new Promise<void>(resolve => { finish = resolve; });
    const request: typeof fetch = async input => {
      await gate;
      return f.request(input);
    };
    const receive = vi.fn();
    const failure = vi.fn();
    const poll = controlRefresh(request, receive, failure);
    const first = poll.run();
    await poll.run();
    poll.cancel();
    finish();
    await first;
    expect(receive).not.toHaveBeenCalled();
    expect(failure).not.toHaveBeenCalled();
    await poll.run();
    expect(receive).toHaveBeenCalledTimes(1);
    poll.dispose();
    await poll.run();
    expect(receive).toHaveBeenCalledTimes(1);
  });
});
