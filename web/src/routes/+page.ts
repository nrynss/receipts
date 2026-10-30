import type { PageLoad } from './$types';

export const load: PageLoad = ({ url }) => ({ fixtureMode: url.searchParams.has('fixture') });
