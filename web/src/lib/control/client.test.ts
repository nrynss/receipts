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
