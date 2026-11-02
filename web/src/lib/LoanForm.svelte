<script lang="ts" module>
	import { todayISO } from './dates.js';
	import type { Direction } from './api.js';
	import { emptyPlan, type PlanValues } from './PlanFields.svelte';

	export type LoanValues = {
		contactId: number | null;
		direction: Direction;
		amount: string;
		startedOn: string;
		purpose: string;
		plan: PlanValues;
		message: string;
		expiresInDays: string;
	};

	/** Default values for a new loan: one payment due in a month. */
	export function emptyLoan(contactId: number | null): LoanValues {
		return {
			contactId,
			direction: 'lent',
			amount: '',
			startedOn: todayISO(),
			purpose: '',
			plan: emptyPlan(),
			message: '',
			expiresInDays: '14'
		};
	}
</script>

<script lang="ts">
	import { ApiError, type Contact, type ScheduleInput } from './api.js';
	import Alert from './Alert.svelte';
	import PlanFields, { toSchedule } from './PlanFields.svelte';
	import SelectField from './SelectField.svelte';
	import TextField from './TextField.svelte';
	import { errorText, t } from './i18n.svelte.js';
	import { toMinor } from './money.js';

	type Payload = {
		contact_id?: number | null;
		direction?: Direction;
		principal: number;
		started_on: string;
		purpose: string;
		schedule: ScheduleInput;
		message?: string;
		expires_in_days?: number;
	};

	let {
		mode,
		currency,
		contacts = [],
		initial,
		onsave
	}: {
		mode: 'create' | 'edit' | 'request';
		currency: string;
		contacts?: Contact[];
		initial: LoanValues;
		onsave: (payload: Payload) => Promise<void>;
	} = $props();

	// svelte-ignore state_referenced_locally
	let v = $state<LoanValues>({ ...initial });
	let fields = $state<Record<string, string>>({});
	let formError = $state('');
	let busy = $state(false);

	const principal = $derived(toMinor(v.amount, currency));

	// Server field names → this form's fields.
	function fieldError(...keys: string[]) {
		for (const k of keys) if (fields[k]) return fields[k];
		return '';
	}

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		fields = {};
		formError = '';
		if (!principal) {
			fields = { principal: 'invalid' };
			return;
		}
		busy = true;
		try {
			const payload: Payload = { principal, started_on: v.startedOn, purpose: v.purpose, schedule: toSchedule(v.plan) };
			if (mode === 'create') Object.assign(payload, { contact_id: v.contactId, direction: v.direction });
			if (mode === 'request') Object.assign(payload, { contact_id: v.contactId, message: v.message, expires_in_days: Number(v.expiresInDays) });
			await onsave(payload);
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else formError = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}
</script>

<form class="md-form loan_form" onsubmit={submit} novalidate>
	{#if mode !== 'edit'}
		<SelectField id="contact" label={t('contact')} hint={mode === 'request' ? t('lender_needs_email') : ''} bind:value={v.contactId} error={fieldError('contact_id')}>
			<option value={null} disabled>{t('choose_contact')}</option>
			{#each contacts as c (c.id)}<option value={c.id} disabled={mode === 'request' && !c.email}>{c.name}{mode === 'request' && c.email ? ` · ${c.email}` : ''}</option>{/each}
		</SelectField>
	{/if}
	{#if mode === 'create'}

		<div class="md-segmented" role="group" aria-label={t('direction')}>
			{#each [['lent', t('i_lent')], ['borrowed', t('i_borrowed')]] as [value, label] (value)}
				<button type="button" class="md-state" aria-pressed={v.direction === value} onclick={() => (v.direction = value as Direction)}>{label}</button>
			{/each}
		</div>
	{/if}

	<TextField id="amount" label="{t('amount')} ({currency})" bind:value={v.amount} error={fieldError('principal')} inputmode="decimal" autocomplete="off" required />
	<TextField id="started_on" label={t('start_date')} type="date" bind:value={v.startedOn} error={fieldError('started_on')} required />
	<TextField id="purpose" label={t('purpose')} bind:value={v.purpose} error={fieldError('purpose')} maxlength={500} />

	<PlanFields bind:value={v.plan} {principal} {currency} {fields} />

	{#if mode === 'request'}
		<TextField id="message" label={t('request_message')} hint={t('request_message_hint')} multiline bind:value={v.message} error={fieldError('message')} maxlength={1000} />
		<SelectField id="expires" label={t('expires_in')} bind:value={v.expiresInDays} error={fieldError('expires_in_days')}>
			{#each ['7', '14', '30'] as n (n)}<option value={n}>{t('days_n', { n })}</option>{/each}
		</SelectField>
	{/if}

	{#if formError}<Alert message={formError} />{/if}
	<div class="md-form__actions">
		<button type="submit" class="md-button md-button--filled md-state" disabled={busy}>{mode === 'request' ? t('send_request') : t('save_loan')}</button>
	</div>
</form>


<style>
	.loan_form {
		max-width: 480px;
	}
</style>
