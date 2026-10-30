import { describe, expect, it } from 'vitest';
import { createOverlayRecovery } from './recovery';

const receipt = { id:'r1', claim:'Will try the route', clip_status:'unavailable' as const, verdict:'undecided' as const, created_at:'2025-01-01T00:00:00Z' };
const snapshot = (sequence: number, appeal?: { receipt_id: string; closes_at: string; uphold_votes: number; overturn_votes: number }) => ({
  epoch:'process-a', channel:'demo', live:true, sequence, receipts:[receipt], current_stamp:receipt, current_appeal:appeal
});
const frame = (sequence: number, name = 'stamp', epoch = 'process-a', extra: Record<string, unknown> = {}) => ({
  name,
  data:JSON.stringify({ epoch, sequence, occurred_at:'2025-01-01T00:00:10Z', ...extra })
});

describe('production overlay recovery composition', () => {
  it('animates a fresh stamp once, then restores the same persisted appeal deadline after reconnect without animation', async () => {
    const appeal = { receipt_id:'r1', closes_at:'2025-01-01T00:01:00Z', uphold_votes:4, overturn_votes:2 };
    let reads = 0;
    const shown: Array<{ state: string; animate?: boolean; appeal?: unknown }> = [];
    const recovery = createOverlayRecovery(async () => {
      reads++;
      return reads === 1 ? snapshot(0) : { ...snapshot(0, appeal), epoch:'process-b' };
    }, display => shown.push(display));

    await recovery.catchUp();
    const stamp = frame(1, 'stamp', 'process-a', { receipt });
    recovery.onFrame(stamp);
    expect(shown.at(-1)).toMatchObject({ state:'stamp', animate:true });
    const animatedCount = shown.filter(display => display.state === 'stamp' && display.animate).length;
    recovery.onFrame(stamp);
    expect(shown.filter(display => display.state === 'stamp' && display.animate)).toHaveLength(animatedCount);

    recovery.onFrame({ name:'reset', data:'{}' });
    expect(reads).toBe(1);
    await recovery.catchUp();
    expect(shown.at(-1)).toMatchObject({ state:'appeal', animate:false, appeal });
    expect(reads).toBe(2);
    recovery.onFrame(frame(1, 'stamp', 'process-a', { receipt }));
    expect(shown.at(-1)).toMatchObject({ state:'appeal', animate:false, appeal });
  });

  it('rebases a vote update into the current appeal without replaying a stamp', async () => {
    const appeal = { receipt_id:'r1', closes_at:'2025-01-01T00:01:00Z', uphold_votes:1, overturn_votes:0 };
    const shown: Array<{ state: string; animate?: boolean; appeal?: unknown }> = [];
    const recovery = createOverlayRecovery(async () => snapshot(0, appeal), display => shown.push(display));
    await recovery.catchUp();
    recovery.onFrame(frame(1, 'status', 'process-a', { appeal: { ...appeal, uphold_votes:2 } }));
    expect(shown.at(-1)).toMatchObject({ state:'appeal', animate:false, appeal:{ ...appeal, uphold_votes:2 } });
  });

  it('clears on disconnect and ignores an unresolved snapshot until the next open', async () => {
    let resolveSnapshot!: (value: ReturnType<typeof snapshot>) => void;
    let reads = 0;
    const shown: Array<{ state: string; animate?: boolean }> = [];
    const recovery = createOverlayRecovery(() => {
      reads++;
      if (reads === 1) return new Promise(resolve => { resolveSnapshot = resolve; });
      return Promise.resolve(snapshot(0));
    }, display => shown.push(display));

    const pending = recovery.catchUp();
    recovery.disconnect();
    resolveSnapshot(snapshot(0));
    await pending;
    expect(shown).toEqual([{ state:'idle' }]);

    await recovery.catchUp();
    expect(shown.at(-1)).toMatchObject({ state:'evidence', animate:false });
  });

  it('clears restored content when the next authoritative snapshot fails', async () => {
    const shown: Array<{ state: string; animate?: boolean }> = [];
    let reads = 0;
    const recovery = createOverlayRecovery(async () => {
      reads++;
      if (reads > 1) throw new Error('snapshot unavailable');
      return snapshot(0);
    }, display => shown.push(display));
    await recovery.catchUp();
    expect(shown.at(-1)?.state).toBe('evidence');
    await recovery.catchUp();
    expect(shown.at(-1)).toEqual({ state:'idle' });
  });

  it('does not publish an in-flight snapshot after disposal', async () => {
    let resolveSnapshot!: (value: ReturnType<typeof snapshot>) => void;
    const shown: Array<{ state: string; animate?: boolean }> = [];
    const recovery = createOverlayRecovery(() => new Promise(resolve => { resolveSnapshot = resolve; }), display => shown.push(display));
    const pending = recovery.catchUp();
    recovery.dispose();
    resolveSnapshot(snapshot(0));
    await pending;
    expect(shown).toEqual([]);
  });

  it('clears restored content when a sequence-gap snapshot fails', async () => {
    const shown: Array<{ state: string; animate?: boolean }> = [];
    let reads = 0;
    const recovery = createOverlayRecovery(async () => {
      reads++;
      if (reads > 1) throw new Error('snapshot unavailable');
      return snapshot(0);
    }, display => shown.push(display));
    await recovery.catchUp();
    recovery.onFrame(frame(3, 'status', 'process-a', { live:true }));
    await new Promise(resolve => setTimeout(resolve, 0));
    expect(reads).toBe(2);
    expect(shown.at(-1)).toEqual({ state:'idle' });
  });
});
