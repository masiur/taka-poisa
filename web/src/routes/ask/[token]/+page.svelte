<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { api, ApiError, type Payout } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Icon from '#lib/Icon.svelte';
	import LanguageSwitch from '#lib/LanguageSwitch.svelte';
	import PlanFields, { emptyPlan, toSchedule } from '#lib/PlanFields.svelte';
	import SelectField from '#lib/SelectField.svelte';
	import TextField from '#lib/TextField.svelte';
	import { todayISO } from '#lib/dates.js';
	import { money } from '#lib/format.svelte.js';
	import { errorText, i18n, t, type Key } from '#lib/i18n.svelte.js';
	import { toMinor } from '#lib/money.js';

	let { data } = $props();
	const info = $derived(data.info);
	const currency = $derived(info?.currency ?? 'BDT');

	// svelte-ignore state_referenced_locally
	let f = $state({
		name: data.info?.name ?? '',
		email: data.info?.email ?? '',
		phone: data.info?.phone ?? '',
		amount: '',
		startedOn: todayISO(),
		purpose: '',
		message: '',
		website: '' // honeypot
	});
	let plan = $state(emptyPlan());
	let payout = $state<Payout>({ method: 'bkash', account_name: '', account_number: '', bank_name: '', branch: '', routing_number: '' });
	let fields = $state<Record<string, string>>({});
	let error = $state('');
	let busy = $state(false);
	let sent = $state(false);
	const principal = $derived(toMinor(f.amount, currency));
	const wallet = $derived(payout.method === 'bkash' || payout.method === 'nagad' || payout.method === 'rocket');

	onMount(() => {
		let chosen = false;
		try {
			chosen = !!localStorage.getItem('locale');
		} catch {
			// storage unavailable
		}
		if (info && !chosen) i18n.locale = info.locale;
	});

	function clearFieldError(e: Event) {
		const id = (e.target as HTMLElement).id;
		const key = id.startsWith('payout_') ? `payout.${id.slice(7)}` : id === 'amount' ? 'principal' : id;
		if (fields[key]) fields = Object.fromEntries(Object.entries(fields).filter(([k]) => k !== key));
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		fields = {};
		error = '';
		if (!principal) {
			fields = { principal: 'invalid' };
			return;
		}
		busy = true;
		try {
			await api('POST', `/public/ask/${page.params.token}`, {
				name: f.name, email: f.email, phone: f.phone, locale: i18n.locale, website: f.website,
				principal, started_on: f.startedOn, purpose: f.purpose, message: f.message,
				schedule: toSchedule(plan), payout
			});
			sent = true;
			window.scrollTo({ top: 0 });
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else error = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>{info ? t('ask_page_title', { owner: info.owner_name }) : t('app_name')}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="ask">
	<div class="top"><LanguageSwitch /></div>
	<main class="card">
		<p class="brand md-title-medium">{t('app_name')}</p>
		{#if !info}
			<Alert message={errorText(data.error)} />
		{:else if sent}
			<h1 class="md-headline-small">{t('ask_page_title', { owner: info.owner_name })}</h1>
			<p class="md-note"><Icon name="check" size={20} />{t('ask_sent', { owner: info.owner_name })}</p>
		{:else}
			<h1 class="md-headline-small">{t('ask_page_title', { owner: info.owner_name })}</h1>
			<p class="md-body-medium md-muted">{t('ask_intro', { owner: info.owner_name })}</p>

			<form class="md-form" onsubmit={submit} oninput={clearFieldError} novalidate>
				<p class="md-section-title">{t('ask_you')}</p>
				<TextField id="name" label={t('your_name')} bind:value={f.name} error={fields.name} autocomplete="name" required maxlength={100} />
				<TextField id="email" label={t('email')} type="email" hint={t('your_email_hint', { owner: info.owner_name })} bind:value={f.email} error={fields.email} autocomplete="email" required />
				<TextField id="phone" label={t('phone')} type="tel" bind:value={f.phone} error={fields.phone} autocomplete="tel" inputmode="tel" />

				<p class="md-section-title">{t('ask_money')}</p>
				<TextField id="amount" label="{t('amount')} ({currency})" hint={info.max_amount ? t('max_amount_hint', { amount: money(info.max_amount, currency) }) : ''}
					bind:value={f.amount} error={fields.principal} inputmode="decimal" autocomplete="off" required />
				<TextField id="started_on" label={t('need_by')} type="date" bind:value={f.startedOn} error={fields.started_on} required />
				<TextField id="purpose" label={t('purpose')} bind:value={f.purpose} error={fields.purpose} maxlength={500} />
				<PlanFields bind:value={plan} {principal} {currency} previewPath="/public/schedule/preview" {fields} />
				<TextField id="message" label={t('request_message')} hint={t('request_message_hint')} multiline bind:value={f.message} error={fields.message} maxlength={1000} />

				<p class="md-section-title">{t('ask_receive')}</p>
				<SelectField id="payout_method" label={t('method_label')} bind:value={payout.method} error={fields['payout.method']}>
					{#each ['bkash', 'nagad', 'rocket', 'bank', 'cash'] as m (m)}<option value={m}>{t(`payout_${m}` as Key)}</option>{/each}
				</SelectField>
				{#if wallet}
					<TextField id="payout_account_number" label={t('wallet_number')} type="tel" bind:value={payout.account_number} error={fields['payout.account_number']} inputmode="tel" required />
				{:else if payout.method === 'bank'}
					<TextField id="payout_account_name" label={t('account_name')} bind:value={payout.account_name} error={fields['payout.account_name']} required maxlength={100} />
					<TextField id="payout_account_number" label={t('account_number')} bind:value={payout.account_number} error={fields['payout.account_number']} inputmode="numeric" required maxlength={40} />
					<TextField id="payout_bank_name" label={t('bank_name')} bind:value={payout.bank_name} error={fields['payout.bank_name']} required maxlength={100} />
					<div class="row">
						<TextField id="payout_branch" label={t('branch')} bind:value={payout.branch} error={fields['payout.branch']} maxlength={100} />
						<TextField id="payout_routing_number" label={t('routing_number')} bind:value={payout.routing_number} error={fields['payout.routing_number']} inputmode="numeric" maxlength={20} />
					</div>
				{/if}

				<!-- Honeypot: hidden from people and screen readers; bots fill it in. -->
				<input class="hp" tabindex="-1" autocomplete="off" aria-hidden="true" bind:value={f.website} name="website" />

				{#if error}<Alert message={error} />{/if}
				<div class="md-form__actions">
					<button type="submit" class="md-button md-button--filled md-button--block md-state" disabled={busy}><Icon name="send" size={18} />{t('ask_send')}</button>
				</div>
			</form>
		{/if}
	</main>
</div>

<style>
	.ask {
		min-height: 100dvh;
		padding: 16px;
		display: grid;
		align-content: start;
		justify-items: center;
		gap: 16px;
		background: var(--md-sys-color-surface-container);
	}

	.top {
		justify-self: end;
	}

	.card {
		--md-field-label-bg: var(--md-sys-color-surface-container-lowest);
		width: 100%;
		max-width: 520px;
		padding: 28px 20px;
		border-radius: var(--md-sys-shape-xl);
		background: var(--md-sys-color-surface-container-lowest);
		display: grid;
		gap: 12px;
	}

	.brand {
		color: var(--md-sys-color-primary);
	}

	.row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	.hp {
		position: absolute;
		left: -10000px;
		width: 1px;
		height: 1px;
		opacity: 0;
	}

	@media (min-width: 600px) {
		.ask {
			padding-top: 40px;
		}
		.card {
			padding: 36px;
		}
	}
</style>
