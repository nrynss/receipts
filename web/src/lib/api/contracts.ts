import { createEventStream, type EventStream } from '@nrynss/chaaya/core';

export type PublicReceipt = { id: string; claim: string; clip_url?: string; clip_status: 'pending' | 'ready' | 'unavailable'; verdict: 'undecided' | 'admitted' | 'upheld' | 'overturned' | 'context_changed' | 'inconclusive'; created_at: string };
export type OverlaySnapshot = { epoch: string; channel: string; live: boolean; receipts: PublicReceipt[]; current_stamp?: PublicReceipt; sequence: number };
export type OverlayEvent = { epoch: string; sequence: number; occurred_at: string; receipt?: PublicReceipt; live?: boolean };
export type ReviewItem = { id: string; claim: string; reason: string; created_at: string };
export type Health = { status: string; mode: string; live_confirmed: boolean; last_event_at?: string };
export type TranscriptSegment = { id: string; session_id: string; speaker_id: string; attribution: 'broadcaster' | 'uncertain'; start_ns: number; end_ns: number; text: string };
export type ChatCommand = { id: string; channel_id: string; session_id: string; user_id: string; login: string; role: 'viewer' | 'moderator' | 'broadcaster'; command: string; arguments: string; received_at: string };
export type ClipResult = { request_id: string; clip_id: string; url: string; created_at: string; status: 'pending' | 'ready' | 'unavailable' | 'failed' };
export type LiveSessionChange = { session_id: string; channel_id: string; state: 'live' | 'offline' | 'ended' | 'unknown'; observed_at: string; confirmed: boolean };

const record = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const str = (v: unknown): v is string => typeof v === 'string';
const time = (v: unknown): v is string => str(v) && Number.isFinite(Date.parse(v));
const valid = (v: unknown, values: readonly string[]): boolean => str(v) && values.includes(v);
function receipt(v: unknown): v is PublicReceipt {
  return record(v) && str(v.id) && str(v.claim) && (v.clip_url === undefined || str(v.clip_url)) && valid(v.clip_status, ['pending','ready','unavailable']) && valid(v.verdict, ['undecided','admitted','upheld','overturned','context_changed','inconclusive']) && time(v.created_at);
}
export function decodeSnapshot(v: unknown): OverlaySnapshot {
  if (!record(v) || !str(v.epoch) || !str(v.channel) || typeof v.live !== 'boolean' || !Array.isArray(v.receipts) || !v.receipts.every(receipt) || (v.current_stamp !== undefined && !receipt(v.current_stamp)) || !Number.isSafeInteger(v.sequence) || Number(v.sequence) < 0) throw new Error('invalid overlay snapshot');
  return v as OverlaySnapshot;
}
export function decodeOverlayEvent(v: unknown): OverlayEvent {
  if (!record(v) || !str(v.epoch) || !Number.isSafeInteger(v.sequence) || Number(v.sequence) <= 0 || !time(v.occurred_at) || (v.receipt !== undefined && !receipt(v.receipt)) || (v.live !== undefined && typeof v.live !== 'boolean')) throw new Error('invalid overlay event');
  return v as OverlayEvent;
}
export function decodeReviewQueue(v: unknown): ReviewItem[] {
  if (!Array.isArray(v) || !v.every(x => record(x) && str(x.id) && str(x.claim) && str(x.reason) && time(x.created_at))) throw new Error('invalid review queue');
  return v as ReviewItem[];
}
export function decodeHealth(v: unknown): Health {
  if (!record(v) || !str(v.status) || !str(v.mode) || typeof v.live_confirmed !== 'boolean' || (v.last_event_at !== undefined && !time(v.last_event_at))) throw new Error('invalid health response');
  return v as Health;
}
export function decodeTranscriptSegment(v: unknown): TranscriptSegment {
  if (!record(v) || !str(v.id) || !str(v.session_id) || !str(v.speaker_id) || !valid(v.attribution,['broadcaster','uncertain']) || !Number.isSafeInteger(v.start_ns) || !Number.isSafeInteger(v.end_ns) || Number(v.start_ns) < 0 || Number(v.end_ns) < Number(v.start_ns) || !str(v.text)) throw new Error('invalid transcript segment');
  return v as TranscriptSegment;
}
export function decodeChatCommand(v: unknown): ChatCommand {
  if (!record(v) || !['id','channel_id','session_id','user_id','login','command','arguments'].every(k => str(v[k])) || !valid(v.role,['viewer','moderator','broadcaster']) || !time(v.received_at)) throw new Error('invalid chat command');
  return v as ChatCommand;
}
export function decodeClipResult(v: unknown): ClipResult {
  if (!record(v) || !['request_id','clip_id','url'].every(k => str(v[k])) || !time(v.created_at) || !valid(v.status,['pending','ready','unavailable','failed'])) throw new Error('invalid clip result');
  return v as ClipResult;
}
export function decodeLiveSessionChange(v: unknown): LiveSessionChange {
  if (!record(v) || !['session_id','channel_id'].every(k => str(v[k])) || !valid(v.state,['live','offline','ended','unknown']) || !time(v.observed_at) || typeof v.confirmed !== 'boolean') throw new Error('invalid live session change');
  return v as LiveSessionChange;
}

// OBS URLs carry no credential; reads use an Authorization header.
export function overlayURL(origin: string): string { return new URL('/api/overlay', origin).toString(); }
export async function fetchOverlaySnapshot(origin: string, credential: string): Promise<OverlaySnapshot> {
  const response = await fetch(overlayURL(origin), { headers: { Authorization: `Bearer ${credential}` } });
  if (!response.ok) throw new Error(`overlay snapshot unavailable (${response.status})`);
  return decodeSnapshot(await response.json());
}
export function connectOverlay(origin: string, credential: string, fetchSnapshot: () => Promise<OverlaySnapshot>, apply: (event: OverlayEvent) => void, replace: (snapshot: OverlaySnapshot) => void): EventStream {
  const rebaser = createOverlayRebaser(fetchSnapshot, apply, replace);
  return createEventStream(new URL('/api/events', origin).toString(), {
    headers: { Authorization: `Bearer ${credential}` },
    events: ['reset','snapshot','stamp','status'],
    catchUp: rebaser.rebase,
    onFrame: event => {
      if (event.name === 'reset') void rebaser.rebase().catch(() => {});
      else rebaser.receive(decodeOverlayEvent(JSON.parse(event.data)));
    }
  });
}

// Subscribe first, then fetch authoritative state. Replayed events are held
// until that fetch finishes and only contiguous newer events are applied.
export function createOverlayRebaser(fetchSnapshot: () => Promise<OverlaySnapshot>, apply: (event: OverlayEvent) => void, replace: (snapshot: OverlaySnapshot) => void) {
  let snapshot: OverlaySnapshot | undefined;
  const pending: OverlayEvent[] = [];
  let flight: Promise<void> | undefined;
  const enqueue = (event: OverlayEvent) => { if (pending.length === 128) pending.shift(); pending.push(event); };
  const receive = (event: OverlayEvent) => {
    if (!snapshot) { enqueue(event); return; }
    if (event.epoch !== snapshot.epoch || event.sequence > snapshot.sequence + 1) { snapshot = undefined; enqueue(event); void rebase().catch(() => {}); return; }
    if (event.sequence > snapshot.sequence) { snapshot = { ...snapshot, sequence: event.sequence }; apply(event); }
  };
  const rebase = (): Promise<void> => {
    if (flight) return flight;
    flight = (async () => {
      try {
        for (let attempt = 0; attempt < 3; attempt++) {
          const next = decodeSnapshot(await fetchSnapshot());
          snapshot = { ...next };
          replace(snapshot);
          let gap = false;
          for (const event of pending.splice(0).sort((a, b) => a.sequence - b.sequence)) {
            if (event.epoch !== snapshot.epoch || event.sequence > snapshot.sequence + 1) { enqueue(event); gap = true; continue; }
            if (event.sequence > snapshot.sequence) { snapshot = { ...snapshot, sequence: event.sequence }; apply(event); }
          }
          if (!gap && pending.length === 0) return;
        }
        throw new Error('overlay stream could not rebase');
      } catch (error) { snapshot = undefined; throw error; }
      finally { flight = undefined; }
    })();
    return flight;
  };
  return { receive, rebase };
}
