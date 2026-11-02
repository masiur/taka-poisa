<script lang="ts">
	import { goto, refreshAll } from '$app/navigation';
	import { api, ApiError, type Payment } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Dialog from '#lib/Dialog.svelte';
	import Icon from '#lib/Icon.svelte';
	import RuleEditor from '#lib/RuleEditor.svelte';
	import SelectField from '#lib/SelectField.svelte';
	import StatusChip from '#lib/StatusChip.svelte';
	import TextField from '#lib/TextField.svelte';
	import { todayISO } from '#lib/dates.js';
	import { day, money, num } from '#lib/format.svelte.js';
	import { errorText, i18n, t, type Key } from '#lib/i18n.svelte.js';
	import { toInput, toMinor } from '#lib/money.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const loan = $derived(data.loan);
	const active = $derived(loan.status === 'active');
	const editable = $derived(loan.status === 'active' || loan.status === 'settled');
	// Someone asked me to lend through a link and I haven't sent the money yet.
	const linkOpen = $derived(loan.origin === 'link' && (loan.status === 'requested' || loan.status === 'approved'));
	const progress = $derived(loan.principal ? Math.round((loan.paid / loan.principal) * 100) : 0);
	const methods = ['cash', 'bkash', 'nagad', 'rocket', 'bank', 'card', 'other'];
	const claims = $derived(loan.payments.filter((p) => p.status === 'pending'));
	const payments = $derived(loan.payments.filter((p) => p.status !== 'pending'));
	const rem = $derived(data.reminders);
	const contactReachable = $derived(rem.direction === 'lent' && !!rem.contact_email && !rem.contact_opted_out);

	async function resolveClaim(p: Payment, confirm: boolean) {
		try {
			await api('POST', `/agreements/${loan.id}/payments/${p.id}/${confirm ? 'confirm' : 'reject'}`);
			snackbar.show(t(confirm ? 'claim_confirmed' : 'claim_rejected'));
			await refreshAll();
		} catch (err) {
			snackbar.show(errorText(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.code) : 'generic'));
		}
	}

	async function sendNow(recipient: 'contact' | 'self') {
		try {
			await api('POST', `/agreements/${loan.id}/reminders/send-now`, { recipient });
			snackbar.show(t('reminder_queued'));
			await refreshAll();
		} catch (err) {
			snackbar.show(errorText(err instanceof ApiError ? err.code : 'generic'));
		}
	}

	function when(iso: string) {
		return new Date(iso).toLocaleString(i18n.locale === 'bn' ? 'bn-BD' : 'en-GB', { dateStyle: 'medium', timeStyle: 'short' });
	}

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

	// Link requests: approve, mark as sent, decline, block.
	let sentOpen = $state(false);
	let sentForm = $state({ method: 'bkash', ref: '', sentOn: todayISO() });
	let sentFields = $state<Record<string, string>>({});
	let declineOpen = $state(false);
	let declineNote = $state('');
	let blockOpen = $state(false);

	async function linkAction(path: string, body: unknown, done: string) {
		try {
			await api('POST', `/agreements/${loan.id}/${path}`, body);
			snackbar.show(done);
			await refreshAll();
			return true;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') sentFields = err.fields;
			else snackbar.show(errorText(err instanceof ApiError ? err.code : 'generic'));
			return false;
		}
	}

	function openSent() {
		const m = loan.payout?.method;
		sentForm = { method: m && m !== 'cash' ? m : 'cash', ref: '', sentOn: todayISO() };
		sentFields = {};
		sentOpen = true;
	}

	async function copy(text: string) {
		await navigator.clipboard.writeText(text);
		snackbar.show(t('copied'));
	}

	async function withdraw() {
		try {
			await api('POST', `/agreements/${loan.id}/withdraw`);
			snackbar.show(t('request_withdrawn'));
			await refreshAll();
		} catch (err) {
			snackbar.show(errorText(err instanceof ApiError ? err.code : 'generic'));
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
	<h1 class="md-headline-small">
		{linkOpen
			? t('asks_to_borrow', { name: loan.contact_name, amount: money(loan.principal, loan.currency) })
			: t(loan.direction === 'lent' ? 'lent_to' : 'borrowed_from', { name: loan.contact_name })}
	</h1>
	{#if editable || linkOpen}
		<a href="/loans/{loan.id}/edit" class="md-icon-button md-state" aria-label={t('edit_loan')} title={t('edit_loan')}><Icon name="edit" /></a>
	{/if}
</header>

{#if loan.origin === 'request' && loan.status !== 'active' && loan.status !== 'settled'}
	<section class="md-note request_note">
		<Icon name={loan.status === 'requested' ? 'schedule' : 'notifications_off'} size={20} />
		<div>
			{#if loan.status === 'requested'}
				<p>{t('waiting_answer', { name: loan.contact_name, date: day((loan.request_expires_at ?? '').slice(0, 10)) })}</p>
				{#if loan.request_message}<p class="md-muted">“{loan.request_message}”</p>{/if}
				<button type="button" class="md-button md-button--text md-state" onclick={withdraw}>{t('withdraw')}</button>
			{:else if loan.status === 'declined'}
				<p>{t('request_declined_note', { name: loan.contact_name })}{loan.decision_note ? ` “${loan.decision_note}”` : ''}</p>
			{:else if loan.status === 'expired'}
				<p>{t('request_expired_note', { date: day((loan.request_expires_at ?? '').slice(0, 10)) })}</p>
			{/if}
		</div>
	</section>
{/if}

{#if loan.origin === 'link' && loan.status !== 'active' && loan.status !== 'settled'}
	<section class="link_request">
		{#if loan.request_message}<p class="md-body-large">“{loan.request_message}”</p>{/if}
		{#if loan.status === 'approved'}<p class="md-note">{t('approved_note')}</p>{/if}
		{#if loan.status === 'declined'}<p class="md-note">{t('request_declined')}{loan.decision_note ? ` · “${loan.decision_note}”` : ''}</p>{/if}
		{#if loan.status === 'expired'}<p class="md-note">{t('request_expired_note', { date: day((loan.request_expires_at ?? '').slice(0, 10)) })}</p>{/if}

		{#if loan.payout}
			<div class="payout">
				<p class="md-label-large">{t('send_to_them')}: {t(`payout_${loan.payout.method}` as Key)}</p>
				{#each [['account_name', loan.payout.account_name], ['account_number', loan.payout.account_number], ['bank_name', loan.payout.bank_name], ['branch', loan.payout.branch], ['routing_number', loan.payout.routing_number]] as [key, value] (key)}
					{#if value}
						<div class="payout__row">
							<span class="md-body-small md-muted">{t((key === 'account_number' && loan.payout.method !== 'bank' ? 'wallet_number' : key) as Key)}</span>
							<span class="md-title-medium">{value}</span>
							<button type="button" class="md-icon-button md-state" title={t('copy_link')} aria-label="{t('copied')}: {value}" onclick={() => copy(value)}><Icon name="content_copy" size={20} /></button>
						</div>
					{/if}
				{/each}
				<p class="md-body-small md-muted">{t('payout_private')}</p>
			</div>
		{/if}

		{#if linkOpen}
			<div class="link_actions">
				<button type="button" class="md-button md-button--filled md-state" onclick={openSent}><Icon name="send" size={18} />{t('mark_sent')}</button>
				{#if loan.status === 'requested'}
					<button type="button" class="md-button md-button--tonal md-state" onclick={() => linkAction('approve', { note: '' }, t('approved_note'))}><Icon name="check" size={18} />{t('approve')}</button>
				{/if}
				<button type="button" class="md-button md-button--text md-state" onclick={() => { declineNote = ''; declineOpen = true; }}>{t('decline')}</button>
				<button type="button" class="md-button md-button--text md-state danger" onclick={() => (blockOpen = true)}>{t('block_requester')}</button>
			</div>
		{/if}
	</section>
{/if}

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
	{#if loan.sent_at}
		<p class="md-body-small md-muted">{t('sent_via', { method: t(`method_${loan.sent_method}` as Key), ref: loan.sent_ref ? ` · ${loan.sent_ref}` : '', date: day(loan.started_on) })}</p>
	{/if}
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
					{loan.installments.length > 1 ? t('installment_n', { n: num(it.seq), count: num(loan.installments.length) }) : t('one_payment')}
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

{#if claims.length > 0}
	<h2 class="md-section-title">{t('pending_claims')}</h2>
	<ul class="md-list claims">
		{#each claims as p (p.id)}
			<li class="md-list__item">
				<span class="md-list__main">
					<span class="md-body-large">{t('claim_says', { name: loan.contact_name, amount: money(p.amount, loan.currency) })}</span>
					<span class="md-body-small md-muted">{day(p.paid_on)} · {t(`method_${p.method}` as Key)}{p.note ? ` · ${p.note}` : ''}</span>
				</span>
				<span class="claim_actions">
					<button type="button" class="md-button md-button--text md-state" onclick={() => resolveClaim(p, false)}>{t('reject_claim')}</button>
					<button type="button" class="md-button md-button--tonal md-state" onclick={() => resolveClaim(p, true)}>{t('confirm_claim')}</button>
				</span>
			</li>
		{/each}
	</ul>
{/if}

<!-- A request that never became a loan has no payments to show. -->
{#if !((loan.origin === 'request' || loan.origin === 'link') && !editable)}
<h2 class="md-section-title">{t('payments')}</h2>
{#if payments.length === 0}
	<p class="md-body-medium md-muted">{t('no_payments')}</p>
{:else}
	<ul class="md-list">
		{#each payments as p (p.id)}
			<li class="md-list__item">
				<span class="md-list__main">
					<span class="md-body-large">{money(p.amount, loan.currency)}</span>
					<span class="md-body-small md-muted">{day(p.paid_on)} · {t(`method_${p.method}` as Key)}{p.note ? ` · ${p.note}` : ''}</span>
				</span>
				{#if p.status === 'rejected'}
					<span class="md-chip md-chip--neutral">{t('status_rejected')}</span>
				{:else if editable}
					<button type="button" class="md-icon-button md-state" aria-label={t('delete')} title={t('delete')}
						onclick={() => { deleting = p; confirmError = ''; deleteOpen = true; }}>
						<Icon name="delete" />
					</button>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

{#if active}
	<h2 class="md-section-title">{t('reminders')}</h2>
	<div class="reminders">
		{#if !data.emailEnabled}
			<p class="md-note"><Icon name="notifications_off" size={20} />{t('email_off')}</p>
		{:else if rem.direction === 'borrowed'}
			<p class="md-note"><Icon name="notifications" size={20} />{t('borrowed_reminders')}</p>
		{:else if rem.contact_opted_out}
			<p class="md-note"><Icon name="notifications_off" size={20} />{t('contact_opted_out', { name: loan.contact_name })}</p>
		{:else if !rem.contact_email}
			<p class="md-note"><Icon name="mail" size={20} /><span>{t('contact_no_email', { name: loan.contact_name })} <a href="/contacts/{loan.contact_id}/edit">{t('edit')}</a></span></p>
		{/if}
		<p class="md-body-small md-muted">{t('reminders_hint')}</p>

		{#key rem.rules}
			<RuleEditor
				rules={rem.rules}
				contactName={loan.contact_name}
				defaultHour={data.sendHour}
				onsave={async (rules) => {
					await api('PUT', `/agreements/${loan.id}/reminders`, { rules });
					snackbar.show(t('reminders_saved'));
					await refreshAll();
				}}
			/>
		{/key}

		{#if rem.upcoming.length > 0}
			<p class="md-label-large">{t('next_reminders')}</p>
			<ul class="sends md-body-medium">
				{#each rem.upcoming as s (s.id)}
					<li><Icon name="schedule" size={18} />{when(s.scheduled_for)} · {s.recipient === 'self' ? t('to_me') : t('to_contact', { name: loan.contact_name })}{s.tone === 'firm' ? ` · ${t('tone_firm')}` : ''}</li>
				{/each}
			</ul>
		{/if}

		<div class="send_now">
			{#if contactReachable}
				<button type="button" class="md-button md-button--tonal md-state" onclick={() => sendNow('contact')}><Icon name="send" size={18} />{t('send_now_contact', { name: loan.contact_name })}</button>
			{/if}
			<button type="button" class="md-button md-button--outlined md-state" onclick={() => sendNow('self')}><Icon name="send" size={18} />{t('send_now_me')}</button>
		</div>

		{#if rem.recent.length > 0}
			<p class="md-label-large">{t('recent_reminders')}</p>
			<ul class="sends md-body-medium">
				{#each rem.recent as s (s.id)}
					<li>
						<span class="md-chip md-chip--{s.status === 'sent' ? 'success' : s.status === 'failed' ? 'error' : 'neutral'}">{t(`send_status_${s.status}` as Key)}</span>
						{when(s.sent_at ?? s.scheduled_for)} · {s.recipient === 'self' ? t('to_me') : t('to_contact', { name: loan.contact_name })}
						{#if s.skip_reason}<span class="md-muted"> · {t(`skip_${s.skip_reason}` as Key)}</span>{/if}
					</li>
				{/each}
			</ul>
		{/if}
	</div>
{/if}

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

<Dialog bind:open={sentOpen} title={t('mark_sent_title')}>
	<p class="md-body-small">{t('mark_sent_hint')}</p>
	<form id="sent_form" class="md-form" novalidate onsubmit={async (e) => {
		e.preventDefault();
		if (await linkAction('mark-sent', { method: sentForm.method, ref: sentForm.ref, sent_on: sentForm.sentOn }, t('request_marked_sent'))) sentOpen = false;
	}}>
		<SelectField id="sent_method" label={t('method')} bind:value={sentForm.method} error={sentFields.method}>
			{#each methods as m (m)}<option value={m}>{t(`method_${m}` as Key)}</option>{/each}
		</SelectField>
		<TextField id="sent_ref" label={t('ref')} bind:value={sentForm.ref} error={sentFields.ref} maxlength={100} />
		<TextField id="sent_on" label={t('sent_on')} type="date" bind:value={sentForm.sentOn} error={sentFields.sent_on} required />
	</form>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (sentOpen = false)}>{t('cancel')}</button>
		<button type="submit" form="sent_form" class="md-button md-button--filled md-state">{t('mark_sent')}</button>
	{/snippet}
</Dialog>

<Dialog bind:open={declineOpen} title={t('decline_title')}>
	<TextField id="decline_note" label={t('decision_note', { name: loan.contact_name })} multiline bind:value={declineNote} maxlength={500} />
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (declineOpen = false)}>{t('cancel')}</button>
		<button type="button" class="md-button md-button--filled md-state" onclick={async () => {
			if (await linkAction('decline', { note: declineNote }, t('request_declined'))) declineOpen = false;
		}}>{t('decline')}</button>
	{/snippet}
</Dialog>

<Dialog bind:open={blockOpen} title={t('block_title', { name: loan.contact_name })}>
	<p class="md-body-medium">{t('block_body')}</p>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (blockOpen = false)}>{t('cancel')}</button>
		<button type="button" class="md-button md-button--filled md-state" onclick={async () => {
			if (await linkAction('block', {}, t('blocked'))) blockOpen = false;
		}}>{t('block_requester')}</button>
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
	.request_note {
		margin-bottom: 16px;
	}

	.link_request {
		display: grid;
		gap: 12px;
		margin-bottom: 16px;
	}

	.payout {
		display: grid;
		gap: 4px;
		padding: 16px;
		border-radius: var(--md-sys-shape-lg);
		background: var(--md-sys-color-primary-container);
		color: var(--md-sys-color-on-primary-container);
	}

	.payout__row {
		display: grid;
		grid-template-columns: minmax(96px, auto) 1fr auto;
		align-items: center;
		gap: 8px;
		overflow-wrap: anywhere;
	}

	.link_actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}

	.request_note p {
		margin: 0 0 4px;
	}

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

	.claims {
		background: var(--md-sys-color-primary-container);
		border-radius: var(--md-sys-shape-lg);
	}

	.claim_actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 4px;
	}

	.reminders {
		display: grid;
		gap: 12px;
	}

	.sends {
		display: grid;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.sends li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}

	.send_now {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
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
