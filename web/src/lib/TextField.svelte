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
		...rest
	}: {
		id: string;
		label: string;
		value?: string;
		type?: 'text' | 'email' | 'password';
		hint?: string;
		error?: string;
	} & Omit<HTMLInputAttributes, 'id' | 'value' | 'type'> = $props();

	let reveal = $state(false);
	const supportId = $derived(error || hint ? `${id}-support` : undefined);
</script>

<div class="md-field" class:md-field--error={!!error} class:md-field--populated={value !== ''}>
	<div class="md-field__box">
		<input
			{id}
			class="md-field__input"
			type={type === 'password' && reveal ? 'text' : type}
			bind:value
			aria-invalid={!!error}
			aria-describedby={supportId}
			{...rest}
		/>
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
