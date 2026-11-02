<script lang="ts">
	// Self-hosted fonts (no Google Fonts CDN): Roboto for Latin, Noto Sans Bengali for Bangla.
	import '@fontsource-variable/roboto';
	import '@fontsource-variable/noto-sans-bengali';
	import '../app.css';
	import { i18n } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data, children } = $props();

	$effect(() => {
		if (data.me) i18n.locale = data.me.user.locale;
	});
</script>

{@render children()}

<div aria-live="polite">
	{#if snackbar.message}<div class="md-snackbar">{snackbar.message}</div>{/if}
</div>
