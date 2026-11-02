<script lang="ts">
	import Icon from './Icon.svelte';
	import { i18n, type Locale } from './i18n.svelte.js';

	const options: { value: Locale; label: string }[] = [
		{ value: 'bn', label: 'বাংলা' },
		{ value: 'en', label: 'English' }
	];
</script>

<div class="md-segmented" role="group" aria-label="Language / ভাষা">
	{#each options as o (o.value)}
		<button type="button" class="md-state" aria-pressed={i18n.locale === o.value} onclick={() => (i18n.locale = o.value)}>
			{#if i18n.locale === o.value}<Icon name="check" size={18} />{/if}
			{o.label}
		</button>
	{/each}
</div>
