import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import Page, { demoDisplay, emptyLore, loreReply } from './+page.svelte';

describe('recorded judge demo', () => {
  it('labels the demo and exposes bounded, accessible replay controls', () => {
    const body = render(Page).body;
    expect(body).toContain('RECORDED DEMO');
    expect(body).toContain('NO PROVIDER CALLS');
    expect(body).toContain('Replay scenario');
    expect(body).toContain('Reset demo');
    expect(body).toContain('aria-live="polite"');
    expect(body).toContain('expires automatically');
  });

  it('keeps the stage verdict and simulated lore aligned with the recorded upheld result', () => {
    expect(demoDisplay('verdict', { stage: 'ready' }).receipt?.verdict).toBe('upheld');
    const recordedLore = 'Lore: 1 total | 0 open, 1 broken, 0 overturned, 0 context changed, 0 inconclusive. Claims: “I will never take that shortcut again.” (broken)';
    expect(loreReply({ stage: 'ready' })).toBe(emptyLore);
    expect(loreReply({ stage: 'complete', verdict: 'upheld', lore: recordedLore })).toBe(recordedLore);
    expect(loreReply({ stage: 'complete', verdict: 'upheld' })).toBe(emptyLore);
  });
});
