<svelte:head><meta name="description" content="The stream remembers what you said." /></svelte:head>
<main>
	<p class="eyebrow">RECEIPTS · OVERLAY</p>
	<h1>The stream remembers what you said.</h1>
	<p class="empty">No receipts yet. When chat clips a promise, it will appear here.</p>
</main>

<style>
	:global(body) { margin: 0; background: #111315; color: #f4efe5; font: 16px/1.5 system-ui, sans-serif; }
	main { max-width: 42rem; margin: 12vh auto; padding: 2rem; }
	.eyebrow { color: #f2a65a; font-size: .75rem; letter-spacing: .16em; }
	h1 { max-width: 13ch; font-size: clamp(2.5rem, 8vw, 5rem); line-height: .98; }
	.empty { color: #a7a7a4; border-top: 1px solid #3a3a37; padding-top: 1.5rem; }
</style>
