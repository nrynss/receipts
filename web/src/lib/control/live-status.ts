const liveStates = new Set(['not_listening', 'session_active', 'listening', 'paused']);

export type LiveConfirmation = 'Confirmed' | 'Offline' | 'Unknown';

// The account status endpoint performs the fresh Twitch observation. A stale,
// failed, or unrecognized read must not imply that persisted session state is live.
export function liveConfirmation(state: unknown, fresh = true): LiveConfirmation {
  if (!fresh || typeof state !== 'string') return 'Unknown';
  if (liveStates.has(state)) return 'Confirmed';
  if (state === 'offline') return 'Offline';
  return 'Unknown';
}
