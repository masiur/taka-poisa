<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import AuthLayout from '#lib/AuthLayout.svelte';
	import GoogleButton from '#lib/GoogleButton.svelte';
	import TextField from '#lib/TextField.svelte';
	import { errorText, t } from '#lib/i18n.svelte.js';

	let { data } = $props();

	let email = $state('');
	let password = $state('');
	let submitError = $state('');
	let busy = $state(false);

	// Errors from the Google callback arrive as ?error=code.
	const redirectError = $derived(page.url.searchParams.get('error'));
	const formError = $derived(submitError || (redirectError ? errorText(redirectError) : ''));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		submitError = '';
		try {
			await api('POST', '/login', { email, password });
			await goto('/', { invalidateAll: true });
		} catch (err) {
			submitError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('login_title')} · {t('app_name')}</title></svelte:head>

<AuthLayout title={t('login_title')}>
	<div class="md-form">
		{#if formError}<Alert message={formError} />{/if}
		{#if data.google}
			<GoogleButton label={t('continue_with_google')} onclick={() => (location.href = '/api/auth/google/start?mode=login')} />
			<p class="md-divider-label">{t('or')}</p>
		{/if}
		<form class="md-form" onsubmit={submit}>
			<TextField id="email" label={t('email')} type="email" bind:value={email} autocomplete="username" required />
			<TextField id="password" label={t('password')} type="password" bind:value={password} autocomplete="current-password" required />
			<div class="md-form__actions">
				<button type="submit" class="md-button md-button--filled md-button--block md-state" disabled={busy}>{t('login')}</button>
			</div>
		</form>
	</div>
</AuthLayout>
