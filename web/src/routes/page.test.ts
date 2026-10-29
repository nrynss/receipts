import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import Page from './+page.svelte';
import Overlay from '$lib/overlay/Overlay.svelte';
import { fixture } from '$lib/overlay/fixtures';

describe('overlay', () => {
  it('starts transparent and idle', () => { expect(render(Page).body).not.toContain('RECEIPT'); });
  it('renders each fixture without exposing private material', () => {
    for (const state of ['stamp', 'evidence', 'appeal', 'verdict'] as const) {
      const body = render(Overlay, { props: { display: fixture(state) } }).body;
      expect(body).toContain('long way around'); expect(body).toContain('@river');
      expect(body).not.toContain('review');
    }
    expect(render(Overlay, { props: { display: fixture('evidence') } }).body).toContain('CLIP UNAVAILABLE');
  });
});
