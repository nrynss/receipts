import { describe, expect, it, vi } from 'vitest';
import { createActions, csrf } from './client';

describe('owner control actions', () => {
  it('preserves action identity after failure and prevents a different decision', async () => {
    const request = vi.fn().mockResolvedValueOnce(new Response(null, { status: 503 })).mockResolvedValueOnce(new Response(null, { status: 204 }));
    const actions = createActions(request, () => 'receipts_csrf=token', () => 'stable-key');
    const body = { receipt_id: 'r', receipt_decision: 'admit' };
    await expect(actions.send('/api/control/decision', body, true)).rejects.toThrow();
    await expect(actions.send('/api/control/decision', { ...body, receipt_decision: 'appeal' }, true)).rejects.toThrow();
    await actions.send('/api/control/decision', body, true);
    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.calls[0][1].body).toBe(request.mock.calls[1][1].body);
    expect(JSON.parse(request.mock.calls[1][1].body).action_key).toBe('stable-key');
    expect(request.mock.calls[1][1].headers['X-CSRF-Token']).toBe('token');
  });
  it('does not submit twice while a request is pending', async () => {
    let finish!: (response: Response) => void;
    const request = vi.fn(() => new Promise<Response>((resolve) => { finish = resolve; }));
    const actions = createActions(request, () => '', () => 'key');
    const first = actions.send('/api/control/review', { review_id: 'r', decision: 'approve' }, true);
    expect(await actions.send('/api/control/review', { review_id: 'r', decision: 'approve' }, true)).toBe(false);
    finish(new Response(null, { status: 204 }));
    await first;
    expect(request).toHaveBeenCalledTimes(1);
  });
  it('handles malformed cookies safely', () => { expect(csrf('receipts_csrf=%')).toBe(''); });
});

it('restores an uncertain action per owner and channel without persisting the reason', async () => {
  const data = new Map<string, string>();
  const storage = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => { data.set(k, v); }, removeItem: (k: string) => { data.delete(k); } } as Storage;
  const body = { receipt_id: 'r', receipt_decision: 'context_changed', note: 'private context' };
  const fail = vi.fn().mockResolvedValue(new Response(null, { status: 503 }));
  const first = createActions(fail, () => '', () => 'original', storage);
  first.setScope('owner', 'channel');
  await expect(first.send('/api/control/decision', body, true)).rejects.toThrow();
  expect([...data.values()].join()).not.toContain('private context');
  const request = vi.fn().mockResolvedValue(new Response(null, { status: 204 }));
  const reloaded = createActions(request, () => '', () => 'new', storage);
  reloaded.setScope('other', 'channel');
  await reloaded.send('/api/control/decision', body, true);
  expect(JSON.parse(request.mock.calls[0][1].body).action_key).toBe('new');
  reloaded.setScope('owner', 'channel');
  await expect(reloaded.send('/api/control/decision', { ...body, receipt_decision: 'admit' }, true)).rejects.toThrow();
  await reloaded.send('/api/control/decision', body, true);
  expect(JSON.parse(request.mock.calls[1][1].body).action_key).toBe('original');
  expect(data.size).toBe(0);
});
