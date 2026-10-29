<script lang="ts">
  import { untrack } from 'svelte';
  import Overlay from '../src/lib/overlay/Overlay.svelte';
  import type { Display } from '../src/lib/overlay/model';
  let { initial }: { initial: Display } = $props();
  let current = $state(untrack(() => initial));
  export function setDisplay(next: Display) { current = next; }
</script>
<Overlay display={current} />
