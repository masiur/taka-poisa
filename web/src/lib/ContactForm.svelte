<script lang="ts" module>
	export type ContactValues = { name: string; email: string; phone: string; locale: 'bn' | 'en'; notes: string };
</script>

<script lang="ts">
	import { ApiError } from './api.js';
	import Alert from './Alert.svelte';
	import SelectField from './SelectField.svelte';
	import TextField from './TextField.svelte';
	import { errorText, t } from './i18n.svelte.js';


	let {
		initial,
		onsave
	}: { initial: ContactValues; onsave: (values: ContactValues) => Promise<void> } = $props();

	// svelte-ignore state_referenced_locally
	let form = $state<ContactValues>({ ...initial });
	let fields = $state<Record<string, string>>({});
	let formError = $state('');
	let busy = $state(false);

	function clearFieldError(e: Event) {
		const id = (e.target as HTMLElement).id;
		if (fields[id]) fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== id));
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		fields = {};
		formError = '';
		try {
			await onsave(form);
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else formError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}
</script>

<form class="md-form contact_form" onsubmit={submit} oninput={clearFieldError} novalidate>
	<TextField id="name" label={t('name')} bind:value={form.name} error={fields.name} required maxlength={100} autocomplete="off" />
	<TextField id="phone" label={t('phone')} type="tel" hint={t('phone_hint')} bind:value={form.phone} error={fields.phone} inputmode="tel" autocomplete="off" />
	<TextField id="email" label={t('email')} type="email" bind:value={form.email} error={fields.email} autocomplete="off" />
	<SelectField id="locale" label={t('contact_language')} bind:value={form.locale} error={fields.locale}>
		<option value="bn">বাংলা</option>
		<option value="en">English</option>
	</SelectField>
	<TextField id="notes" label={t('notes')} multiline bind:value={form.notes} error={fields.notes} maxlength={2000} />
	{#if formError}<Alert message={formError} />{/if}
	<div class="md-form__actions">
		<button type="submit" class="md-button md-button--filled md-state" disabled={busy}>{t('save')}</button>
	</div>
</form>

<style>
	.contact_form {
		max-width: 480px;
	}
</style>
