import type { OverlaySnapshot, PublicAppeal, PublicReceipt } from '$lib/api/contracts';

export type OverlayState = 'idle' | 'stamp' | 'evidence' | 'appeal' | 'verdict';
export type DisplayReceipt = PublicReceipt;
export type Display = { state: OverlayState; receipt?: DisplayReceipt; animate?: boolean; appeal?: PublicAppeal };

export function receiptAge(createdAt: string, now: number): string {
  const days = Math.max(0, Math.floor((now - Date.parse(createdAt)) / 86400000));
  if (!Number.isFinite(days)) return 'age unknown';
  if (days === 0) return 'today';
  if (days === 1) return '1 day ago';
  return `${days} days ago`;
}

// Only canonical Twitch clip URLs are accepted; query strings and credentials
// never reach the iframe. The parent is the actual overlay hostname.
export function clipEmbed(url: string | undefined, hostname: string): string | undefined {
  if (!url || !hostname || !/^[a-z0-9.-]+$/i.test(hostname)) return;
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || parsed.port) return;
    let slug: string | undefined;
    if (parsed.hostname === 'clips.twitch.tv') slug = /^\/([A-Za-z0-9_-]+)\/?$/.exec(parsed.pathname)?.[1];
    if (parsed.hostname === 'www.twitch.tv') slug = /^\/[A-Za-z0-9_]+\/clip\/([A-Za-z0-9_-]+)\/?$/.exec(parsed.pathname)?.[1];
    if (!slug || slug.toLowerCase() === 'embed') return;
    const embed = new URL('https://clips.twitch.tv/embed');
    embed.searchParams.set('clip', slug);
    embed.searchParams.set('parent', hostname);
    embed.searchParams.set('autoplay', 'false');
    return embed.toString();
  } catch { return; }
}

export function snapshotDisplay(receipt?: PublicReceipt): Display {
  return receipt ? { state: receipt.verdict === 'undecided' ? 'evidence' : 'verdict', receipt, animate: false } : { state: 'idle' };
}

// T3.3 may pass an authoritative rebase here; snapshot state never animates.
// A fresh stamp event can instead render { state: "stamp", receipt, animate: true };
// the caller owns event sequencing and supplies later evidence/appeal/verdict states.
export function displayFromSnapshot(snapshot: OverlaySnapshot): Display {
  const receipt = snapshot.current_stamp;
  if (!receipt) return { state: 'idle' };
  if (snapshot.current_appeal?.receipt_id === receipt.id) return { state: 'appeal', receipt, appeal: snapshot.current_appeal, animate: false };
  return snapshotDisplay(receipt);
}
