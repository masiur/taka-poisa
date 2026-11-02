<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';
	import { errorText } from './i18n.svelte.js';

	let {
		id,
		label,
		value = $bindable(),
		hint = '',
		error = '',
		disabled = false,
		children
	}: {
		id: string;
		label: string;
		value: T;
		hint?: string;
		error?: string;
		disabled?: boolean;
		children: Snippet;
	} = $props();

	const supportId = $derived(error || hint ? `${id}-support` : undefined);
</script>

<!-- A native select keeps the platform picker on phones; it is styled as an M3 outlined field. -->
<div class="md-field md-field--populated" class:md-field--error={!!error} class:md-field--disabled={disabled}>
	<div class="md-field__box">
		<select {id} class="md-field__input" bind:value {disabled} aria-invalid={!!error} aria-describedby={supportId}>
			{@render children()}
		</select>
		<label class="md-field__label" for={id}>{label}</label>
		<span class="md-field__trailing md-field__trailing--select"><Icon name="arrow_drop_down" /></span>
	</div>
	{#if supportId}
		<p class="md-field__support" id={supportId}>{error ? errorText(error) : hint}</p>
	{/if}
</div>
