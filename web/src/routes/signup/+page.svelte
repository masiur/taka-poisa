<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import AuthLayout from '#lib/AuthLayout.svelte';
	import TextField from '#lib/TextField.svelte';
	import { errorText, i18n, t } from '#lib/i18n.svelte.js';

	let name = $state('');
	let email = $state('');
	let password = $state('');
	let fields = $state<Record<string, string>>({});
	let submitError = $state('');
	let busy = $state(false);

	// Editing a field clears its error.
	function clearFieldError(e: Event) {
		const id = (e.target as HTMLElement).id;
		if (fields[id]) fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== id));
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		fields = {};
		submitError = '';
		try {
			await api('POST', '/signup', { name, email, password, locale: i18n.locale });
			await goto('/', { refreshAll: true });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else submitError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head><title>{t('signup_title')} · {t('app_name')}</title></svelte:head>

<AuthLayout title={t('signup_title')} intro={t('signup_intro')}>
	<div class="md-form" oninput={clearFieldError}>
		{#if submitError}<Alert message={submitError} />{/if}
		<form class="md-form" onsubmit={submit} novalidate>
			<TextField id="name" label={t('your_name')} bind:value={name} error={fields.name} autocomplete="name" required maxlength={100} />
			<TextField id="email" label={t('email')} type="email" bind:value={email} error={fields.email} autocomplete="email" required />
			<TextField id="password" label={t('password')} type="password" hint={t('password_hint')} bind:value={password} error={fields.password} autocomplete="new-password" required minlength={8} />
			<div class="md-form__actions">
				<button type="submit" class="md-button md-button--filled md-button--block md-state" disabled={busy}>{t('create_account')}</button>
			</div>
		</form>
		<p class="md-body-medium switch">{t('have_account')} <a href="/login">{t('login')}</a></p>
	</div>
</AuthLayout>

<style>
	.switch {
		margin: 0;
		text-align: center;
	}
</style>
