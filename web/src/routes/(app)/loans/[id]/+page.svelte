<script lang="ts">
	import { goto, refreshAll } from '$app/navigation';
	import { api, ApiError, type Payment } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Dialog from '#lib/Dialog.svelte';
	import Icon from '#lib/Icon.svelte';
	import SelectField from '#lib/SelectField.svelte';
	import StatusChip from '#lib/StatusChip.svelte';
	import TextField from '#lib/TextField.svelte';
	import { todayISO } from '#lib/dates.js';
	import { day, money } from '#lib/format.svelte.js';
	import { errorText, t, type Key } from '#lib/i18n.svelte.js';
	import { toInput, toMinor } from '#lib/money.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const loan = $derived(data.loan);
	const active = $derived(loan.status === 'active');
	const editable = $derived(loan.status === 'active' || loan.status === 'settled');
	const progress = $derived(loan.principal ? Math.round((loan.paid / loan.principal) * 100) : 0);
	const methods = ['cash', 'bkash', 'nagad', 'rocket', 'bank', 'card', 'other'];

	// Record payment dialog
	let payOpen = $state(false);
	let pay = $state({ amount: '', paidOn: todayISO(), method: 'cash', note: '' });
	let payFields = $state<Record<string, string>>({});
	let payError = $state('');
	let payBusy = $state(false);

	function openPay() {
		pay = { amount: toInput(loan.next_amount || loan.remaining, loan.currency), paidOn: todayISO(), method: 'cash', note: '' };
		payFields = {};
		payError = '';
		payOpen = true;
	}

	async function savePayment(e: SubmitEvent) {
		e.preventDefault();
		const amount = toMinor(pay.amount, loan.currency);
		if (!amount) {
			payFields = { amount: 'invalid' };
			return;
		}
		payBusy = true;
		payFields = {};
		payError = '';
		try {
			await api('POST', `/agreements/${loan.id}/payments`, { amount, paid_on: pay.paidOn, method: pay.method, note: pay.note });
			payOpen = false;
			snackbar.show(t('payment_saved'));
			await refreshAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') payFields = err.fields;
			else payError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			payBusy = false;
		}
	}

	// Delete payment / cancel loan confirmations
	let deleting = $state<Payment | null>(null);
	let deleteOpen = $state(false);
	let cancelOpen = $state(false);
	let confirmError = $state('');

	async function deletePayment() {
		if (!deleting) return;
		confirmError = '';
		try {
			await api('DELETE', `/agreements/${loan.id}/payments/${deleting.id}`);
			deleteOpen = false;
			snackbar.show(t('payment_deleted'));
			await refreshAll();
		} catch (err) {
			confirmError = errorText(err instanceof ApiError ? err.code : 'generic');
		}
	}

	async function cancelLoan() {
		confirmError = '';
		try {
			await api('POST', `/agreements/${loan.id}/cancel`);
			cancelOpen = false;
			snackbar.show(t('loan_cancelled'));
			await refreshAll();
		} catch (err) {
			confirmError = errorText(err instanceof ApiError ? err.code : 'generic');
		}
	}

	function activityText(action: string, d: Record<string, unknown>) {
		const key = `act_${action}` as Key;
		const amount = typeof d.amount === 'number' ? money(d.amount, loan.currency) : '';
		return t(key, { amount });
	}
</script>

<svelte:head><title>{loan.contact_name} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/contacts/{loan.contact_id}" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t(loan.direction === 'lent' ? 'lent_to' : 'borrowed_from', { name: loan.contact_name })}</h1>
	{#if editable}
		<a href="/loans/{loan.id}/edit" class="md-icon-button md-state" aria-label={t('edit_loan')} title={t('edit_loan')}><Icon name="edit" /></a>
	{/if}
</header>

<section class="summary">
	<div class="summary__top">
		<div>
			<p class="md-headline-small" class:md-money-in={loan.direction === 'lent'}>{money(loan.remaining, loan.currency)}</p>
			<p class="md-body-medium md-muted">{t('paid_of', { paid: money(loan.paid, loan.currency), total: money(loan.principal, loan.currency) })}</p>
		</div>
		<StatusChip status={loan.status} />
	</div>
	<div class="md-progress" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100} aria-label={t('payments')}>
		<span style:width="{progress}%"></span>
	</div>
	{#if loan.purpose}<p class="md-body-medium">{loan.purpose}</p>{/if}
	<p class="md-body-small md-muted">{t('start_date')}: {day(loan.started_on)}</p>
	{#if active}
		<div class="actions">
			<button type="button" class="md-button md-button--filled md-state" onclick={openPay}><Icon name="payments" size={18} />{t('record_payment')}</button>
		</div>
	{/if}
</section>

<h2 class="md-section-title">{t('schedule_preview')}</h2>
<ul class="md-list">
	{#each loan.installments as it (it.id)}
		<li class="md-list__item installment">
			<span class="md-list__main">
				<span class="md-body-large">{day(it.due_on)}</span>
				<span class="md-body-small md-muted">
					{loan.installments.length > 1 ? t('installment_n', { n: String(it.seq), count: String(loan.installments.length) }) : t('one_payment')}
					{#if it.status === 'partial' || (it.paid > 0 && it.remaining > 0)} · {t('remaining', { amount: money(it.remaining, loan.currency) })}{/if}
				</span>
			</span>
			<span class="md-list__trailing">
				<span class="md-title-medium">{money(it.amount, loan.currency)}</span>
				{#if loan.status === 'active' || it.status === 'paid'}<StatusChip status={it.status} />{/if}
			</span>
		</li>
	{/each}
</ul>

<h2 class="md-section-title">{t('payments')}</h2>
{#if loan.payments.length === 0}
	<p class="md-body-medium md-muted">{t('no_payments')}</p>
{:else}
	<ul class="md-list">
		{#each loan.payments as p (p.id)}
			<li class="md-list__item">
				<span class="md-list__main">
					<span class="md-body-large">{money(p.amount, loan.currency)}</span>
					<span class="md-body-small md-muted">{day(p.paid_on)} · {t(`method_${p.method}` as Key)}{p.note ? ` · ${p.note}` : ''}</span>
				</span>
				{#if editable}
					<button type="button" class="md-icon-button md-state" aria-label={t('delete')} title={t('delete')}
						onclick={() => { deleting = p; confirmError = ''; deleteOpen = true; }}>
						<Icon name="delete" />
					</button>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

<details class="history">
	<summary class="md-section-title"><Icon name="history" size={18} />{t('history')}</summary>
	<ul class="md-list">
		{#each loan.activity as a (a.id)}
			<li class="md-list__item compact">
				<span class="md-list__main">
					<span class="md-body-medium">{activityText(a.action, a.data)}</span>
					<span class="md-body-small md-muted">{a.actor_name} · {new Date(a.created_at).toLocaleString()}</span>
				</span>
			</li>
		{/each}
	</ul>
</details>

{#if editable}
	<button type="button" class="md-button md-button--text md-state cancel_loan" onclick={() => { confirmError = ''; cancelOpen = true; }}>{t('cancel_loan')}</button>
{/if}

<Dialog bind:open={payOpen} title={t('record_payment')}>
	<form id="pay_form" class="md-form" onsubmit={savePayment} novalidate>
		<TextField id="pay_amount" label="{t('amount')} ({loan.currency})" bind:value={pay.amount} error={payFields.amount} inputmode="decimal" autocomplete="off" required />
		<TextField id="pay_date" label={t('paid_on')} type="date" bind:value={pay.paidOn} error={payFields.paid_on} required />
		<SelectField id="pay_method" label={t('method')} bind:value={pay.method} error={payFields.method}>
			{#each methods as m (m)}<option value={m}>{t(`method_${m}` as Key)}</option>{/each}
		</SelectField>
		<TextField id="pay_note" label={t('note')} hint={t('note_hint')} bind:value={pay.note} error={payFields.note} maxlength={500} />
		{#if payError}<Alert message={payError} />{/if}
	</form>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (payOpen = false)}>{t('cancel')}</button>
		<button type="submit" form="pay_form" class="md-button md-button--filled md-state" disabled={payBusy}>{t('save')}</button>
	{/snippet}
</Dialog>

<Dialog bind:open={deleteOpen} title={t('delete_payment')}>
	{#if deleting}<p class="md-body-medium">{t('delete_payment_body', { amount: money(deleting.amount, loan.currency), date: day(deleting.paid_on) })}</p>{/if}
	{#if confirmError}<Alert message={confirmError} />{/if}
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (deleteOpen = false)}>{t('cancel')}</button>
		<button type="button" class="md-button md-button--text md-state danger" onclick={deletePayment}>{t('delete')}</button>
	{/snippet}
</Dialog>

<Dialog bind:open={cancelOpen} title={t('cancel_loan')}>
	<p class="md-body-medium">{t('cancel_loan_body')}</p>
	{#if confirmError}<Alert message={confirmError} />{/if}
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (cancelOpen = false)}>{t('keep_loan')}</button>
		<button type="button" class="md-button md-button--text md-state danger" onclick={cancelLoan}>{t('cancel_loan')}</button>
	{/snippet}
</Dialog>

<style>
	.summary {
		display: grid;
		gap: 12px;
		padding: 16px;
		border-radius: var(--md-sys-shape-lg);
		background: var(--md-sys-color-surface-container-low);
	}

	.summary__top {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 16px;
	}

	.actions {
		display: flex;
		gap: 8px;
	}

	.installment {
		min-height: 64px;
	}

	.compact {
		min-height: 56px;
	}

	.history summary {
		display: flex;
		align-items: center;
		gap: 8px;
		cursor: pointer;
		list-style: none;
	}

	.cancel_loan,
	.danger {
		color: var(--md-sys-color-error);
	}

	.cancel_loan {
		margin-top: 24px;
	}
</style>
