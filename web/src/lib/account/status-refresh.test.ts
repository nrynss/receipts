import { describe, expect, it } from 'vitest';
import { statusRefresh } from './status-refresh';

describe('private status refresh', () => {
  it('reports failed refreshes as unknown', async () => {
    let state = 'listening';
    const poll = statusRefresh(async () => new Response('', {status: 401}), () => { state = 'listening'; }, () => { state = 'unknown'; });
    await poll.run(); expect(state).toBe('unknown');
  });
  it('does not let late status overwrite stop or disposal', async () => {
    let resolve!: (value: Response) => void;
    let signal: AbortSignal | null | undefined;
    const poll = statusRefresh((_input, init) => { signal = init?.signal; return new Promise((r) => { resolve = r; }); }, () => { throw new Error('stale update'); }, () => { throw new Error('stale failure'); });
    const pending = poll.run(); poll.cancel(); expect(signal?.aborted).toBe(true);
    resolve(new Response('{}')); await pending;
    const second = poll.run(); poll.dispose(); expect(signal?.aborted).toBe(true);
    resolve(new Response('{}')); await second;
  });
});
