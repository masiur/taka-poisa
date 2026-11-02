<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError, type AccountSettings } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import SelectField from '#lib/SelectField.svelte';
	import TextField from '#lib/TextField.svelte';
	import Icon from '#lib/Icon.svelte';
	import { errorText, t, type Key } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();

	// A local, editable copy of the loaded settings.
	let form = $state<AccountSettings>(untrack(() => structuredClone(data.settings)));
	let fields = $state<Record<string, string>>({});
	let saving = $state(false);
	let formError = $state('');

	const readOnly = $derived(data.me?.account.role !== 'owner');

	// Editing a field clears its error.
	function clearFieldError(e: Event) {
		const id = (e.target as HTMLElement).id;
		if (fields[id]) fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== id));
	}
	const currencies = ['BDT', 'INR', 'PKR', 'NPR', 'USD', 'EUR', 'GBP', 'AED', 'SAR', 'MYR', 'SGD'];
	const timezones = Intl.supportedValuesOf('timeZone');
	const hours = Array.from({ length: 24 }, (_, h) => h);

	function hourLabel(h: number) {
		return new Date(2000, 0, 1, h).toLocaleTimeString(form.locale === 'bn' ? 'bn-BD' : 'en-GB', {
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		fields = {};
		formError = '';
		try {
			await api('PUT', '/account', form);
			snackbar.show(t('saved'));
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else formError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			saving = false;
		}
	}
</script>

<svelte:head><title>{t('settings_title')} · {t('app_name')}</title></svelte:head>

<h1 class="md-headline-small">{t('settings_title')}</h1>
{#if readOnly}<p class="md-body-medium md-muted">{t('owner_only')}</p>{/if}

<form onsubmit={submit} oninput={clearFieldError} onchange={clearFieldError} class="md-form settings">
	<fieldset disabled={readOnly}>
		<TextField id="name" label={t('book_name')} bind:value={form.name} error={fields.name} required maxlength={100} />
		<SelectField id="currency" label={t('currency')} bind:value={form.currency} error={fields.currency} disabled={readOnly}>
			{#each currencies.includes(form.currency) ? currencies : [form.currency, ...currencies] as c (c)}
				<option value={c}>{c}</option>
			{/each}
		</SelectField>
		<SelectField id="timezone" label={t('timezone')} bind:value={form.timezone} error={fields.timezone} disabled={readOnly}>
			{#each timezones.includes(form.timezone) ? timezones : [form.timezone, ...timezones] as tz (tz)}
				<option value={tz}>{tz}</option>
			{/each}
		</SelectField>
		<SelectField id="send_hour" label={t('send_hour')} hint={t('send_hour_hint')} bind:value={form.send_hour} error={fields.send_hour} disabled={readOnly}>
			{#each hours as h (h)}<option value={h}>{hourLabel(h)}</option>{/each}
		</SelectField>
		<SelectField id="locale" label={t('email_language')} hint={t('email_language_hint')} bind:value={form.locale} error={fields.locale} disabled={readOnly}>
			<option value="bn">বাংলা</option>
			<option value="en">English</option>
		</SelectField>
		{#if formError}<Alert message={formError} />{/if}
		<div class="md-form__actions">
			<button type="submit" class="md-button md-button--filled md-state" disabled={saving}>
				{saving ? t('saving') : t('save')}
			</button>
		</div>
	</fieldset>
</form>

<section class="export">
	<h2 class="md-section-title">{t('export')}</h2>
	<p class="md-body-medium md-muted">{t('export_hint')}</p>
	<div class="export__links">
		{#each ['contacts', 'agreements', 'installments', 'payments'] as kind (kind)}
			<a class="md-button md-button--outlined md-state" href="/api/export/{kind}" download>
				<Icon name="download" size={18} />{t(`export_${kind}` as Key)}
			</a>
		{/each}
	</div>
</section>

<style>
	.settings {
		max-width: 480px;
		margin-top: 24px;
	}

	.export {
		margin-top: 16px;
	}

	.export__links {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-top: 12px;
	}
</style>
