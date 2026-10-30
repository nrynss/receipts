import { describe, expect, it } from 'vitest';
import { clipEmbed, receiptAge, snapshotDisplay } from './model';

describe('overlay display helpers', () => {
  it('accepts only Twitch clips and sets the actual parent', () => {
    const url = clipEmbed('https://clips.twitch.tv/OriginalSlug?secret=discard', 'overlay.example');
    expect(url).toContain('parent=overlay.example'); expect(url).not.toContain('secret');
    for (const bad of ['https://clips.twitch.tv.evil.test/slug', 'http://clips.twitch.tv/slug', 'https://user@clips.twitch.tv/slug', 'https://clips.twitch.tv/embed']) expect(clipEmbed(bad, 'overlay.example')).toBeUndefined();
  });
  it('uses elapsed whole days and does not animate a recovered stamp', () => {
    expect(receiptAge('2025-01-01T00:00:00Z', Date.parse('2025-01-02T01:00:00Z'))).toBe('1 day ago');
    expect(snapshotDisplay({ id:'r', claim:'claim', clip_status:'pending', verdict:'undecided', created_at:'2025-01-01T00:00:00Z' }).animate).toBe(false);
  });
});
