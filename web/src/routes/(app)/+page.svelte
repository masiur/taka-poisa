<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { api, ApiError, type RequestView } from '#lib/api.js';
	import Fab from '#lib/Fab.svelte';
	import Icon from '#lib/Icon.svelte';
	import LoanRow from '#lib/LoanRow.svelte';
	import { snackbar } from '#lib/snackbar.svelte.js';
	import StatusChip from '#lib/StatusChip.svelte';
	import { day, initial, money, num } from '#lib/format.svelte.js';
	import { errorText, t } from '#lib/i18n.svelte.js';

	let { data } = $props();
	const o = $derived(data.overview);

	async function answer(r: RequestView, accept: boolean) {
		try {
			await api('POST', `/requests/incoming/${r.id}/${accept ? 'accept' : 'decline'}`, { note: '' });
			snackbar.show(t(accept ? 'request_accepted' : 'request_declined'));
			await refreshAll();
		} catch (err) {
			snackbar.show(errorText(err instanceof ApiError ? err.code : 'generic'));
		}
	}
</script>

<svelte:head><title>{t('app_name')}</title></svelte:head>

<h1 class="md-headline-small">{t('welcome', { name: data.me?.user.name ?? '' })}</h1>

<div class="totals">
	<section class="md-card md-card--outlined total">
		<p class="md-label-large md-muted">{t('you_will_get')}</p>
		<p class="md-headline-small md-money-in">{money(o.receivable, o.currency)}</p>
		{#if o.overdue_receivable > 0}
			<p class="md-body-small md-money-out">{t('overdue_amount', { amount: money(o.overdue_receivable, o.currency) })}</p>
		{/if}
	</section>
	<section class="md-card md-card--outlined total">
		<p class="md-label-large md-muted">{t('you_owe')}</p>
		<p class="md-headline-small">{money(o.payable, o.currency)}</p>
		{#if o.overdue_payable > 0}
			<p class="md-body-small md-money-out">{t('overdue_amount', { amount: money(o.overdue_payable, o.currency) })}</p>
		{/if}
	</section>
</div>

<a href="/requests/new" class="md-button md-button--tonal md-state ask"><Icon name="send" size={18} />{t('ask_for_loan')}</a>

{#if data.incoming.length > 0}
	<h2 class="md-section-title">{t('incoming_requests')}</h2>
	<ul class="md-list incoming">
		{#each data.incoming as r (r.id)}
			<li class="md-list__item">
				<span class="md-avatar">{initial(r.borrower_name)}</span>
				<span class="md-list__main">
					<span class="md-body-large">{t('asks_to_borrow', { name: r.borrower_name, amount: money(r.principal, r.currency) })}</span>
					<span class="md-body-small md-muted">{r.message || r.purpose || t('rq_until', { date: day(r.expires_at.slice(0, 10)) })}</span>
				</span>
				<span class="answers">
					<button type="button" class="md-button md-button--text md-state" onclick={() => answer(r, false)}>{t('decline')}</button>
					<button type="button" class="md-button md-button--filled md-state" onclick={() => answer(r, true)}>{t('accept')}</button>
				</span>
			</li>
		{/each}
	</ul>
{/if}

{#if data.sentRequests.length > 0}
	<h2 class="md-section-title">{t('your_requests')}</h2>
	<ul class="md-list">
		{#each data.sentRequests as loan (loan.id)}<li><LoanRow {loan} /></li>{/each}
	</ul>
{/if}

<h2 class="md-section-title">{t('due_soon')}</h2>
{#if o.due.length === 0}
	<p class="md-body-medium md-muted empty">{o.open_loans === 0 ? t('no_loans') : t('nothing_due')}</p>
{:else}
	<ul class="md-list">
		{#each o.due as item (`${item.agreement_id}-${item.seq}`)}
			<li>
				<a class="md-list__item md-state" href="/loans/{item.agreement_id}">
					<span class="md-avatar">{initial(item.contact_name)}</span>
					<span class="md-list__main">
						<span class="md-body-large">{item.contact_name}</span>
						<span class="md-body-medium md-muted">
							{item.direction === 'lent' ? t('i_lent') : t('i_borrowed')} ·
							{item.count > 1 ? t('installment_n', { n: num(item.seq), count: num(item.count) }) : t('one_payment')}
						</span>
					</span>
					<span class="md-list__trailing">
						<span class="md-title-medium" class:md-money-in={item.direction === 'lent'}>{money(item.remaining, o.currency)}</span>
						{#if item.due_on < o.today}
							<StatusChip status="overdue" />
						{:else if item.due_on === o.today}
							<StatusChip status="due_today" />
						{:else}
							<span class="md-body-small md-muted">{day(item.due_on)}</span>
						{/if}
					</span>
				</a>
			</li>
		{/each}
	</ul>
{/if}

<Fab href="/loans/new" label={t('new_loan')} />

<style>
	.totals {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
		gap: 12px;
		margin-top: 16px;
	}

	.total {
		display: grid;
		gap: 4px;
	}

	.empty {
		padding: 24px 0;
	}

	.ask {
		margin-top: 16px;
	}

	.incoming {
		background: var(--md-sys-color-primary-container);
		border-radius: var(--md-sys-shape-lg);
	}

	.answers {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 4px;
	}
</style>
