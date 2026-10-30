import { describe, expect, it } from 'vitest';
import { liveConfirmation } from './live-status';

describe('liveConfirmation', () => {
  it.each(['not_listening', 'session_active', 'listening', 'paused'])(
    'confirms fresh verified live state %s',
    (state) => expect(liveConfirmation(state)).toBe('Confirmed')
  );

  it('reports a fresh offline observation', () => {
    expect(liveConfirmation('offline')).toBe('Offline');
  });

  it.each(['unknown', 'missing_grant', 'unavailable', 'capacity', 'live'])(
    'preserves Unknown for non-confirming state %s',
    (state) => expect(liveConfirmation(state)).toBe('Unknown')
  );

  it('preserves Unknown for stale, failed, or malformed observations', () => {
    expect(liveConfirmation('listening', false)).toBe('Unknown');
    expect(liveConfirmation(undefined)).toBe('Unknown');
    expect(liveConfirmation(null)).toBe('Unknown');
  });
});
