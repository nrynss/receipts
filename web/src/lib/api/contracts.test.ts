import { describe, expect, it } from 'vitest';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { createEventStream, parseNamedFrame } from '@nrynss/chaaya/core';
import { createOverlayCallbacks, createOverlayRebaser, decodeChatCommand, decodeClipResult, decodeHealth, decodeLiveSessionChange, decodeOverlayEvent, decodePauseControl, decodeReceiptDecision, decodeReviewControl, decodeReviewQueue, decodeSnapshot, decodeTranscriptSegment, decodeVoidControl, overlayURL, type OverlaySnapshot } from './contracts';

const stamp = { epoch: 'test-epoch', sequence: 8, occurred_at: '2000-01-01T00:00:01Z', receipt: { id: 'r1', claim: 'Will try the route', clip_url: 'https://clips.example/r1', clip_status: 'ready', verdict: 'undecided', created_at: '2000-01-01T00:00:00Z' } };
describe('T1.3 contract decoders', () => {
  it('decodes all Go golden contracts', async () => {
    const files = ['overlay.snapshot.json', 'overlay.stamp.json', 'review.queue.json', 'health.json', 'transcript.segment.json', 'chat.command.json', 'clip.result.json', 'live-session.change.json', 'control.pause.json', 'control.void.json', 'control.review.json', 'control.receipt-decision.json'];
    const dir = fileURLToPath(new URL('../../../../testdata/contracts/', import.meta.url));
    const payloads = await Promise.all(files.map(async f => JSON.parse(await readFile(resolve(dir, f), 'utf8')) as unknown));
    expect(decodeSnapshot(payloads[0])).toMatchObject({ epoch: 'test-epoch', sequence: 7 });
    expect(decodeOverlayEvent(payloads[1])).toMatchObject({ sequence: 8 });
    expect(decodeReviewQueue(payloads[2])).toHaveLength(1);
    expect(decodeHealth(payloads[3]).status).toBe('ok');
    expect(decodeTranscriptSegment(payloads[4]).start_ns).toBe(1_000_000_000);
    expect(decodeChatCommand(payloads[5]).command).toBe('!receipt');
    expect(decodeClipResult(payloads[6]).status).toBe('ready');
    expect(decodeLiveSessionChange(payloads[7]).confirmed).toBe(true);
    expect(decodePauseControl(payloads[8]).paused).toBe(true);
    expect(decodeVoidControl(payloads[9]).receipt_id).toBe('r1');
    expect(decodeReviewControl(payloads[10]).decision).toBe('approve');
    expect(decodeReceiptDecision(payloads[11]).receipt_decision).toBe('context_changed');
  });
  it('rejects malformed payloads and never puts auth in browser URLs', () => {
    expect(() => decodeSnapshot({ ...stamp, receipts: [{}] })).toThrow();
    expect(() => decodeOverlayEvent({ ...stamp, sequence: -1 })).toThrow();
    expect(() => decodeClipResult({ request_id: 'r', clip_id: 'c', url: 'https://clips.example/c', created_at: '2000-01-01T00:00:00Z', status: 'maybe' })).toThrow();
    expect(overlayURL('https://overlay.example')).toBe('https://overlay.example/api/overlay');
    expect(overlayURL('https://overlay.example').includes('token')).toBe(false);
  });
  it('uses Chaaya parser cursor reset so a restarted Keel broker can emit lower IDs', () => {
    const reset = parseNamedFrame('event: reset\nid: 0\ndata: {}\n\n');
    expect(reset.ok).toBe(true);
    if (!reset.ok || reset.value.kind !== 'event') throw new Error('reset frame not parsed');
    expect(reset.value.id).toBe(0);
    expect(reset.value.idSet).toBe(true);
    const fresh = parseNamedFrame('event: stamp\nid: 1\ndata: {}\n\n');
    expect(fresh.ok).toBe(true);
    if (!fresh.ok || fresh.value.kind !== 'event') throw new Error('fresh frame not parsed');
    // prime() exercises Chaaya's actual accepted-frame replay filter and ID
    // cursor update, without needing a DOM or attaching a live fetch loop.
    const stream = createEventStream('/api/events', { events: ['reset', 'stamp'] });
    stream.prime([{ id: 91, name: 'stamp', data: '{}' }]);
    expect(stream.lastEventId).toBe(91);
    stream.prime([reset.value, fresh.value]);
    expect(stream.lastEventId).toBe(1);
    expect(stream.events.map(e => e.name)).toEqual(['stamp', 'reset', 'stamp']);
    stream.close();
  });
  it('rebases from an authoritative snapshot and does not replay an old stamp as new', async () => {
    const applied: number[] = [];
    const replaced: number[] = [];
    const rebaser = createOverlayRebaser(async () => decodeSnapshot({ epoch: 'test-epoch', channel: 'demo', live: true, receipts: [], sequence: 8 }), e => applied.push(e.sequence), s => replaced.push(s.sequence));
    rebaser.receive(decodeOverlayEvent(stamp));
    await rebaser.rebase();
    expect(replaced).toEqual([8]);
    expect(applied).toEqual([]);
    rebaser.receive(decodeOverlayEvent({ ...stamp, sequence: 9 }));
    expect(applied).toEqual([9]);
  });
  it('applies the first post-snapshot stamp once and rebases reconnect replay', async () => {
    let sequence = 0;
    const applied: number[] = [];
    const rebaser = createOverlayRebaser(async () => decodeSnapshot({ epoch: 'test-epoch', channel: 'demo', live: true, receipts: [], sequence }), e => applied.push(e.sequence), () => {});
    await rebaser.rebase();
    rebaser.receive(decodeOverlayEvent({ ...stamp, sequence: 1 }));
    expect(applied).toEqual([1]);
    sequence = 1;
    await rebaser.rebase();
    rebaser.receive(decodeOverlayEvent({ ...stamp, sequence: 1 }));
    rebaser.receive(decodeOverlayEvent({ ...stamp, sequence: 2 }));
    expect(applied).toEqual([1, 2]);
  });
  it('runs one application rebase per Chaaya open and keeps reset transport-only', async () => {
    let sequence = 0;
    let reads = 0;
    const applied: number[] = [];
    const errors: unknown[] = [];
    const callbacks = createOverlayCallbacks(async () => { reads++; return decodeSnapshot({ epoch: 'test-epoch', channel: 'demo', live: true, receipts: [], sequence }); }, e => applied.push(e.sequence), () => {}, e => errors.push(e));
    await callbacks.catchUp();
    expect(reads).toBe(1);
    callbacks.onFrame({ name: 'reset', data: '{}' });
    callbacks.onFrame({ name: 'stamp', data: JSON.stringify({ ...stamp, sequence: 1 }) });
    expect(applied).toEqual([1]);
    expect(reads).toBe(1);
    sequence = 1;
    await callbacks.catchUp(); // Chaaya calls this after a reconnect.
    callbacks.onFrame({ name: 'stamp', data: JSON.stringify({ ...stamp, sequence: 1 }) });
    callbacks.onFrame({ name: 'stamp', data: JSON.stringify({ ...stamp, sequence: 2 }) });
    expect(reads).toBe(2);
    expect(applied).toEqual([1, 2]);
    expect(errors).toEqual([]);
  });
  it('buffers events while rebase is pending, and reports failures then retries on the next event', async () => {
    let resolveRead!: (value: OverlaySnapshot) => void;
    let shouldFail = false;
    let errors = 0;
    const applied: number[] = [];
    const rebaser = createOverlayRebaser(() => shouldFail ? Promise.reject(new Error('offline')) : new Promise(resolve => { resolveRead = resolve; }), e => applied.push(e.sequence), () => {}, () => { errors++; });
    shouldFail = true;
    await expect(rebaser.rebase()).rejects.toThrow('offline');
    expect(errors).toBe(0); // Explicit awaiters receive their own failure.
    const callbackErrors: unknown[] = [];
    const failingCallbacks = createOverlayCallbacks(async () => { throw new Error('snapshot offline'); }, () => {}, () => {}, e => callbackErrors.push(e));
    await failingCallbacks.catchUp();
    expect(callbackErrors).toHaveLength(1);
    shouldFail = false;
    rebaser.receive(decodeOverlayEvent({ ...stamp, sequence: 1 }));
    resolveRead(decodeSnapshot({ epoch: 'test-epoch', channel: 'demo', live: true, receipts: [], sequence: 0 }));
    await rebaser.rebase();
    expect(applied).toEqual([1]);
  });
});
