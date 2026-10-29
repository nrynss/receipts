import { render } from 'svelte/server';
import { describe, expect, it } from 'vitest';
import Page from './+page.svelte';

describe('initial overlay', () => {
	it('shows an empty ledger without claiming that a stream is live', () => {
		const { body } = render(Page);
		expect(body).toContain('No receipts yet');
		expect(body).not.toContain('LIVE SESSION');
	});
});
