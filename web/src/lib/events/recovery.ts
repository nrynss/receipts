import { createOverlayRebaser, decodeOverlayEvent, type OverlaySnapshot } from '$lib/api/contracts';
import { displayFromSnapshot, type Display } from '$lib/overlay/model';

type OverlayFrameName = 'reset' | 'snapshot' | 'stamp' | 'status';

// Bind named transport frames to the authoritative snapshot rebaser. Catch-up
// runs once for each EventSource open; reset only affects the transport cursor.
export function createOverlayRecovery(
  fetchSnapshot: () => Promise<OverlaySnapshot>,
  show: (display: Display) => void,
  onError: (error: unknown) => void = () => {}
) {
  let current: OverlaySnapshot | undefined;
  const names = new Map<string, OverlayFrameName>();
  const key = (epoch: string, sequence: number) => `${epoch}:${sequence}`;

  const rebaser = createOverlayRebaser(fetchSnapshot, event => {
    const eventKey = key(event.epoch, event.sequence);
    const name = names.get(eventKey);
    names.delete(eventKey);
    if (!current) return;
    const next = { ...current, sequence: event.sequence };
    if (event.receipt) {
      next.current_stamp = event.receipt;
      if (name === 'stamp' || event.receipt.verdict !== 'undecided') next.current_appeal = undefined;
    }
    if (event.appeal) next.current_appeal = event.appeal;
    if (event.live !== undefined) next.live = event.live;
    current = next;
    show(name === 'stamp' && event.receipt
      ? { state: 'stamp', receipt: event.receipt, animate: true }
      : displayFromSnapshot(next));
  }, snapshot => {
    current = snapshot;
    for (const eventKey of names.keys()) {
      const separator = eventKey.lastIndexOf(':');
      const epoch = eventKey.slice(0, separator);
      const sequence = Number(eventKey.slice(separator + 1));
      if (epoch !== snapshot.epoch || sequence <= snapshot.sequence) names.delete(eventKey);
    }
    show(displayFromSnapshot(snapshot));
  }, onError);

  const disconnect = () => {
    names.clear();
    current = undefined;
    rebaser.invalidate();
    show({ state: 'idle' });
  };

  return {
    catchUp: async () => {
      names.clear();
      try { await rebaser.rebase(); } catch (error) { disconnect(); onError(error); }
    },
    disconnect,
    dispose: () => {
      names.clear();
      current = undefined;
      rebaser.invalidate();
    },
    onFrame: (frame: { name: string; data: string }) => {
      if (frame.name === 'reset') return;
      try {
        const event = decodeOverlayEvent(JSON.parse(frame.data));
        const eventKey = key(event.epoch, event.sequence);
        if (names.size >= 128) names.delete(names.keys().next().value!);
        names.set(eventKey, frame.name as OverlayFrameName);
        rebaser.receive(event);
      } catch (error) { onError(error); }
    }
  };
}
