<script lang="ts">
	import type { HTMLInputAttributes } from 'svelte/elements';
	import Icon from './Icon.svelte';
	import { errorText, t } from './i18n.svelte.js';

	let {
		id,
		label,
		value = $bindable(''),
		type = 'text',
		hint = '',
		error = '',
		multiline = false,
		...rest
	}: {
		id: string;
		label: string;
		value?: string;
		type?: 'text' | 'email' | 'password' | 'tel' | 'date';
		hint?: string;
		error?: string;
		multiline?: boolean;
	} & Omit<HTMLInputAttributes, 'id' | 'value' | 'type'> = $props();

	let reveal = $state(false);
	const supportId = $derived(error || hint ? `${id}-support` : undefined);
</script>

<div class="md-field" class:md-field--error={!!error} class:md-field--populated={value !== '' || type === 'date'} class:md-field--multiline={multiline}>
	<div class="md-field__box">
		{#if multiline}
			<textarea
				{id}
				class="md-field__input"
				bind:value
				aria-invalid={!!error}
				aria-describedby={supportId}
				maxlength={rest.maxlength}
				rows={4}
			></textarea>
		{:else}
		<input
			{id}
			class="md-field__input"
			type={type === 'password' && reveal ? 'text' : type}
			bind:value
			aria-invalid={!!error}
			aria-describedby={supportId}
			{...rest}
		/>
		{/if}
		<label class="md-field__label" for={id}>{label}</label>
		{#if type === 'password'}
			<span class="md-field__trailing">
				<button
					type="button"
					class="md-icon-button md-state"
					aria-label={reveal ? t('hide_password') : t('show_password')}
					aria-pressed={reveal}
					onclick={() => (reveal = !reveal)}
				>
					<Icon name={reveal ? 'visibility_off' : 'visibility'} />
				</button>
			</span>
		{/if}
	</div>
	{#if supportId}
		<p class="md-field__support" id={supportId}>{error ? errorText(error) : hint}</p>
	{/if}
</div>
