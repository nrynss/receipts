import type { Display, DisplayReceipt, OverlayState } from './model';

const receipt: DisplayReceipt = {
  id: 'fixture-1', claim: 'I will take the long way around every time.',
  clip_status: 'unavailable', verdict: 'undecided', created_at: '2025-01-01T00:00:00Z',
  clipped_by: 'river'
};
export function fixture(state: OverlayState): Display {
  if (state === 'idle') return { state };
  return { state, receipt: state === 'verdict' ? { ...receipt, verdict: 'upheld' } : receipt,
    animate: state === 'stamp', appeal: state === 'appeal' ? { receipt_id: receipt.id, uphold_votes: 28, overturn_votes: 12, closes_at: '2025-01-01T00:01:21Z' } : undefined };
}
