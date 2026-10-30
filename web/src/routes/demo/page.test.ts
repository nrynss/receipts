import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import Page from './+page.svelte';

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
});
