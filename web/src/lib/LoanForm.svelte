<script lang="ts" module>
	import { addMonthsISO, todayISO } from './dates.js';
	import type { Direction } from './api.js';
	export type LoanValues = {
		contactId: number | null;
		direction: Direction;
		amount: string;
		startedOn: string;
		purpose: string;
		plan: 'one' | 'installments';
		dueDate: string;
		count: string;
		frequency: 'monthly' | 'weekly' | 'every_n_days';
		everyDays: string;
		firstDue: string;
	};

	/** Default values for a new loan: one payment due in a month. */
	export function emptyLoan(contactId: number | null): LoanValues {
		const today = todayISO();
		return {
			contactId,
			direction: 'lent',
			amount: '',
			startedOn: today,
			purpose: '',
			plan: 'one',
			dueDate: addMonthsISO(today, 1),
			count: '3',
			frequency: 'monthly',
			everyDays: '15',
			firstDue: addMonthsISO(today, 1)
		};
	}
</script>

<script lang="ts">
	import { api, ApiError, type Contact, type ScheduleInput } from './api.js';
	import Alert from './Alert.svelte';
	import SelectField from './SelectField.svelte';
	import TextField from './TextField.svelte';
	import { day, money } from './format.svelte.js';
	import { errorText, t } from './i18n.svelte.js';
	import { toMinor } from './money.js';


	type Payload = {
		contact_id?: number | null;
		direction?: Direction;
		principal: number;
		started_on: string;
		purpose: string;
		schedule: ScheduleInput;
	};

	let {
		mode,
		currency,
		contacts = [],
		initial,
		onsave
	}: {
		mode: 'create' | 'edit';
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
	let preview = $state<{ due_on: string; amount: number }[]>([]);

	const principal = $derived(toMinor(v.amount, currency));
	const schedule = $derived<ScheduleInput>(
		v.plan === 'one'
			? { mode: 'generate', count: 1, first_due: v.dueDate }
			: {
					mode: 'generate',
					count: Number(v.count),
					first_due: v.firstDue,
					frequency: v.frequency,
					...(v.frequency === 'every_n_days' ? { every_days: Number(v.everyDays) } : {})
				}
	);

	// Ask the server for the exact schedule it would store, a moment after typing stops.
	$effect(() => {
		const body = { principal, schedule };
		if (!body.principal) {
			preview = [];
			return;
		}
		const timer = setTimeout(async () => {
			try {
				const res = await api<{ installments: { due_on: string; amount: number }[] }>('POST', '/schedule/preview', body);
				preview = res.installments;
			} catch {
				preview = [];
			}
		}, 300);
		return () => clearTimeout(timer);
	});

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
			const payload: Payload = { principal, started_on: v.startedOn, purpose: v.purpose, schedule };
			if (mode === 'create') Object.assign(payload, { contact_id: v.contactId, direction: v.direction });
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
	{#if mode === 'create'}
		<SelectField id="contact" label={t('contact')} bind:value={v.contactId} error={fieldError('contact_id')}>
			<option value={null} disabled>{t('choose_contact')}</option>
			{#each contacts as c (c.id)}<option value={c.id}>{c.name}</option>{/each}
		</SelectField>

		<div class="md-segmented" role="group" aria-label={t('direction')}>
			{#each [['lent', t('i_lent')], ['borrowed', t('i_borrowed')]] as [value, label] (value)}
				<button type="button" class="md-state" aria-pressed={v.direction === value} onclick={() => (v.direction = value as Direction)}>{label}</button>
			{/each}
		</div>
	{/if}

	<TextField id="amount" label="{t('amount')} ({currency})" bind:value={v.amount} error={fieldError('principal')} inputmode="decimal" autocomplete="off" required />
	<TextField id="started_on" label={t('start_date')} type="date" bind:value={v.startedOn} error={fieldError('started_on')} required />
	<TextField id="purpose" label={t('purpose')} bind:value={v.purpose} error={fieldError('purpose')} maxlength={500} />

	<p class="md-section-title">{t('repayment')}</p>
	<div class="md-segmented" role="group" aria-label={t('repayment')}>
		<button type="button" class="md-state" aria-pressed={v.plan === 'one'} onclick={() => (v.plan = 'one')}>{t('one_payment')}</button>
		<button type="button" class="md-state" aria-pressed={v.plan === 'installments'} onclick={() => (v.plan = 'installments')}>{t('installments')}</button>
	</div>

	{#if v.plan === 'one'}
		<TextField id="due_date" label={t('due_date')} type="date" bind:value={v.dueDate} error={fieldError('schedule.first_due')} required />
	{:else}
		<div class="row">
			<TextField id="count" label={t('count')} bind:value={v.count} error={fieldError('schedule.count')} inputmode="numeric" required />
			<SelectField id="frequency" label={t('frequency')} bind:value={v.frequency} error={fieldError('schedule.frequency')}>
				<option value="monthly">{t('monthly')}</option>
				<option value="weekly">{t('weekly')}</option>
				<option value="every_n_days">{t('every_n_days')}</option>
			</SelectField>
		</div>
		{#if v.frequency === 'every_n_days'}
			<TextField id="every_days" label={t('every_days')} bind:value={v.everyDays} error={fieldError('schedule.every_days')} inputmode="numeric" />
		{/if}
		<TextField id="first_due" label={t('first_due')} type="date" bind:value={v.firstDue} error={fieldError('schedule.first_due')} required />
	{/if}

	{#if fieldError('schedule.items', 'schedule.mode')}
		<Alert message={errorText(fieldError('schedule.items', 'schedule.mode'))} />
	{/if}

	{#if preview.length > 0}
		<section class="md-card md-card--filled preview" aria-live="polite">
			<p class="md-label-large">{t('schedule_preview')}</p>
			<ol class="md-body-medium">
				{#each preview as it, i (i)}
					<li><span>{day(it.due_on)}</span><span>{money(it.amount, currency)}</span></li>
				{/each}
			</ol>
		</section>
	{/if}

	{#if formError}<Alert message={formError} />{/if}
	<div class="md-form__actions">
		<button type="submit" class="md-button md-button--filled md-state" disabled={busy}>{t('save_loan')}</button>
	</div>
</form>


<style>
	.loan_form {
		max-width: 480px;
	}

	.row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	.preview {
		background: var(--md-sys-color-surface-container);
	}

	.preview ol {
		display: grid;
		gap: 4px;
		margin: 8px 0 0;
		padding: 0;
		list-style: none;
		max-height: 240px;
		overflow: auto;
	}

	.preview li {
		display: flex;
		justify-content: space-between;
		gap: 16px;
	}
</style>
