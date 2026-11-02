<script lang="ts">
	import { page } from '$app/state';
	import { refreshAll } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, ApiError, type LoanSummary } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Dialog from '#lib/Dialog.svelte';
	import Icon from '#lib/Icon.svelte';
	import LanguageSwitch from '#lib/LanguageSwitch.svelte';
	import SelectField from '#lib/SelectField.svelte';
	import StatusChip from '#lib/StatusChip.svelte';
	import TextField from '#lib/TextField.svelte';
	import { todayISO } from '#lib/dates.js';
	import { day, money } from '#lib/format.svelte.js';
	import { errorText, i18n, t, type Key } from '#lib/i18n.svelte.js';
	import { toInput, toMinor } from '#lib/money.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const st = $derived(data.statement);
	const token = $derived(page.params.token);
	const owed = $derived(st ? st.loans.filter((l) => l.status === 'active').reduce((sum, l) => sum + l.remaining, 0) : 0);
	const methods = ['cash', 'bkash', 'nagad', 'rocket', 'bank', 'card', 'other'];

	// Show the page in the contact's language, unless they already chose one on this device.
	onMount(() => {
		let chosen = false;
		try {
			chosen = !!localStorage.getItem('locale');
		} catch {
			// storage unavailable
		}
		if (st && !chosen) i18n.locale = st.locale;
		if (page.url.searchParams.get('unsubscribe') === '1' && st?.has_email && !st.opted_out) unsubOpen = true;
	});

	// "I've paid"
	let claimOpen = $state(false);
	let claimLoan = $state<LoanSummary | null>(null);
	let claim = $state({ amount: '', paidOn: todayISO(), method: 'bkash', note: '' });
	let claimFields = $state<Record<string, string>>({});
	let claimError = $state('');
	let claimBusy = $state(false);

	function openClaim(l: LoanSummary) {
		claimLoan = l;
		claim = { amount: toInput(l.next_amount || l.remaining, l.currency), paidOn: todayISO(), method: 'bkash', note: '' };
		claimFields = {};
		claimError = '';
		claimOpen = true;
	}

	async function sendClaim(e: SubmitEvent) {
		e.preventDefault();
		if (!claimLoan) return;
		const amount = toMinor(claim.amount, claimLoan.currency);
		if (!amount) {
			claimFields = { amount: 'invalid' };
			return;
		}
		claimBusy = true;
		claimError = '';
		try {
			await api('POST', `/public/statement/${token}/claims`, {
				agreement_id: claimLoan.id, amount, paid_on: claim.paidOn, method: claim.method, note: claim.note
			});
			claimOpen = false;
			snackbar.show(t('st_claim_sent', { owner: st?.owner_name ?? '' }));
			await refreshAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') claimFields = err.fields;
			else claimError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			claimBusy = false;
		}
	}

	// Unsubscribe
	let unsubOpen = $state(false);
	async function unsubscribe() {
		try {
			await api('POST', `/public/statement/${token}/optout`);
			unsubOpen = false;
			await refreshAll();
		} catch (err) {
			snackbar.show(errorText(err instanceof ApiError ? err.code : 'generic'));
		}
	}
</script>

<svelte:head>
	<title>{st ? t('st_title', { owner: st.owner_name }) : t('app_name')}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="statement">
	<div class="top"><LanguageSwitch /></div>
	<main class="card">
		<p class="brand md-title-medium">{t('app_name')}</p>
		{#if !st}
			<Alert message={errorText(data.error)} />
		{:else}
			<h1 class="md-headline-small">{t('st_title', { owner: st.owner_name })}</h1>
			<p class="md-body-medium md-muted">{t('st_intro', { owner: st.owner_name })}</p>

			<section class="total">
				{#if owed > 0}
					<p class="md-label-large md-muted">{t('st_total', { owner: st.owner_name })}</p>
					<p class="md-headline-small">{money(owed, st.currency)}</p>
				{:else}
					<p class="md-title-large paid"><Icon name="check" />{t('st_all_paid')}</p>
				{/if}
			</section>

			{#each st.loans as l (l.id)}
				<section class="loan">
					<div class="loan__head">
						<div>
							<h2 class="md-title-medium">{t('st_loan', { date: day(l.started_on) })}</h2>
							{#if l.purpose}<p class="md-body-small md-muted">{l.purpose}</p>{/if}
						</div>
						<StatusChip status={l.status} />
					</div>
					<p class="md-body-medium">{t('paid_of', { paid: money(l.paid, l.currency), total: money(l.principal, l.currency) })}</p>

					<ul class="md-list">
						{#each l.installments as it (it.id)}
							<li class="md-list__item row">
								<span class="md-list__main">
									<span class="md-body-large">{day(it.due_on)}</span>
									{#if it.paid > 0 && it.remaining > 0}<span class="md-body-small md-muted">{t('remaining', { amount: money(it.remaining, l.currency) })}</span>{/if}
								</span>
								<span class="md-list__trailing">
									<span class="md-title-medium">{money(it.amount, l.currency)}</span>
									<StatusChip status={it.status} />
								</span>
							</li>
						{/each}
					</ul>

					{#if l.payments.length > 0}
						<p class="md-label-large md-muted">{t('payments')}</p>
						<ul class="payments md-body-medium">
							{#each l.payments as p (p.id)}
								<li>
									{money(p.amount, l.currency)} · {day(p.paid_on)} · {t(`method_${p.method}` as Key)}
									{#if p.status === 'pending'}<span class="md-chip md-chip--warning">{t('status_pending')}</span>{/if}
								</li>
							{/each}
						</ul>
					{/if}

					{#if l.status === 'active'}
						<button type="button" class="md-button md-button--filled md-state" onclick={() => openClaim(l)}><Icon name="payments" size={18} />{t('st_i_paid')}</button>
					{/if}
				</section>
			{/each}

			<footer class="md-body-small md-muted">
				{#if st.opted_out}
					<p>{t('st_unsubscribed')}</p>
				{:else if st.has_email}
					<button type="button" class="md-button md-button--text md-state" onclick={() => (unsubOpen = true)}>
						<Icon name="notifications_off" size={18} />{t('st_unsubscribe')}
					</button>
				{/if}
			</footer>
		{/if}
	</main>
</div>

{#if st}
	<Dialog bind:open={claimOpen} title={t('st_claim_title', { owner: st.owner_name })}>
		<p class="md-body-small">{t('st_claim_hint', { owner: st.owner_name })}</p>
		<form id="claim_form" class="md-form" onsubmit={sendClaim} novalidate>
			<TextField id="claim_amount" label="{t('amount')} ({st.currency})" bind:value={claim.amount} error={claimFields.amount} inputmode="decimal" autocomplete="off" required />
			<TextField id="claim_date" label={t('paid_on')} type="date" bind:value={claim.paidOn} error={claimFields.paid_on} required />
			<SelectField id="claim_method" label={t('method')} bind:value={claim.method} error={claimFields.method}>
				{#each methods as m (m)}<option value={m}>{t(`method_${m}` as Key)}</option>{/each}
			</SelectField>
			<TextField id="claim_note" label={t('note')} hint={t('note_hint')} bind:value={claim.note} error={claimFields.note} maxlength={500} />
			{#if claimError}<Alert message={claimError} />{/if}
		</form>
		{#snippet actions()}
			<button type="button" class="md-button md-button--text md-state" onclick={() => (claimOpen = false)}>{t('cancel')}</button>
			<button type="submit" form="claim_form" class="md-button md-button--filled md-state" disabled={claimBusy}>{t('st_i_paid')}</button>
		{/snippet}
	</Dialog>

	<Dialog bind:open={unsubOpen} title={t('st_unsubscribe')}>
		<p class="md-body-medium">{t('st_unsubscribe_confirm', { owner: st.owner_name })}</p>
		{#snippet actions()}
			<button type="button" class="md-button md-button--text md-state" onclick={() => (unsubOpen = false)}>{t('cancel')}</button>
			<button type="button" class="md-button md-button--filled md-state" onclick={unsubscribe}>{t('st_unsubscribe')}</button>
		{/snippet}
	</Dialog>
{/if}

<style>
	.statement {
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
		max-width: 560px;
		padding: 28px 20px;
		border-radius: var(--md-sys-shape-xl);
		background: var(--md-sys-color-surface-container-lowest);
		display: grid;
		gap: 12px;
	}

	.brand {
		color: var(--md-sys-color-primary);
	}

	.total {
		padding: 16px;
		border-radius: var(--md-sys-shape-lg);
		background: var(--md-sys-color-surface-container-low);
	}

	.paid {
		display: flex;
		align-items: center;
		gap: 8px;
		color: var(--md-sys-color-tertiary);
	}

	.loan {
		display: grid;
		gap: 8px;
		padding-top: 16px;
		border-top: 1px solid var(--md-sys-color-outline-variant);
	}

	.loan__head {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 12px;
	}

	.loan button {
		justify-self: start;
	}

	.row {
		min-height: 56px;
		padding: 4px 0;
	}

	.payments {
		display: grid;
		gap: 4px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.payments li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}

	footer {
		margin-top: 8px;
	}

	@media (min-width: 600px) {
		.statement {
			padding-top: 40px;
		}
		.card {
			padding: 36px;
		}
	}
</style>
