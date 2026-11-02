<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import AuthLayout from '#lib/AuthLayout.svelte';
	import GoogleButton from '#lib/GoogleButton.svelte';
	import TextField from '#lib/TextField.svelte';
	import { errorText, i18n, t } from '#lib/i18n.svelte.js';

	let { data } = $props();

	let name = $state('');
	let email = $state('');
	let password = $state('');
	let accountName = $state('');
	let fields = $state<Record<string, string>>({});
	let submitError = $state('');
	let busy = $state(false);

	const redirectError = $derived(page.url.searchParams.get('error'));

	// Editing a field clears its error.
	function clearFieldError(e: Event) {
		const id = (e.target as HTMLElement).id;
		if (fields[id]) fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== id));
	}
	const formError = $derived(submitError || (redirectError ? errorText(redirectError) : ''));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		fields = {};
		submitError = '';
		try {
			await api('POST', '/setup', { name, email, password, account_name: accountName, locale: i18n.locale });
			await goto('/', { refreshAll: true });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else submitError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}

	// Google supplies name and email; the ledger name and language come from this page.
	function setupWithGoogle() {
		if (!accountName.trim()) {
			fields = { account_name: 'required' };
			document.getElementById('account_name')?.focus();
			return;
		}
		const q = new URLSearchParams({ mode: 'setup', account_name: accountName, locale: i18n.locale });
		location.href = `/api/auth/google/start?${q}`;
	}
</script>

<svelte:head><title>{t('setup_title')}</title></svelte:head>

<AuthLayout title={t('setup_title')} intro={t('setup_intro')}>
	<div class="md-form" oninput={clearFieldError}>
		{#if formError}<Alert message={formError} />{/if}
		<TextField id="account_name" label={t('book_name')} hint={t('book_name_hint')} bind:value={accountName} error={fields.account_name} required maxlength={100} />

		{#if data.google}
			<GoogleButton label={t('setup_with_google')} onclick={setupWithGoogle} />
			<p class="md-divider-label">{t('or_with_password')}</p>
		{/if}

		<form class="md-form" onsubmit={submit} novalidate>
			<TextField id="name" label={t('your_name')} bind:value={name} error={fields.name} autocomplete="name" required maxlength={100} />
			<TextField id="email" label={t('email')} type="email" bind:value={email} error={fields.email} autocomplete="email" required />
			<TextField id="password" label={t('password')} type="password" hint={t('password_hint')} bind:value={password} error={fields.password} autocomplete="new-password" required minlength={8} />
			<div class="md-form__actions">
				<button type="submit" class="md-button md-button--filled md-button--block md-state" disabled={busy}>{t('create_account')}</button>
			</div>
		</form>
	</div>
</AuthLayout>
