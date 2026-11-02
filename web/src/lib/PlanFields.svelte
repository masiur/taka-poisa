<script lang="ts" module>
	import type { ScheduleInput } from './api.js';
	import { addMonthsISO, todayISO } from './dates.js';

	export type PlanValues = {
		kind: 'one' | 'installments';
		dueDate: string;
		count: string;
		frequency: 'monthly' | 'weekly' | 'every_n_days';
		everyDays: string;
		firstDue: string;
	};

	/** One payment due in a month; installments default to 3 monthly. */
	export function emptyPlan(): PlanValues {
		const inAMonth = addMonthsISO(todayISO(), 1);
		return { kind: 'one', dueDate: inAMonth, count: '3', frequency: 'monthly', everyDays: '15', firstDue: inAMonth };
	}

	export function toSchedule(p: PlanValues): ScheduleInput {
		if (p.kind === 'one') return { mode: 'generate', count: 1, first_due: p.dueDate };
		return {
			mode: 'generate',
			count: Number(p.count),
			first_due: p.firstDue,
			frequency: p.frequency,
			...(p.frequency === 'every_n_days' ? { every_days: Number(p.everyDays) } : {})
		};
	}
</script>

<script lang="ts">
	import { api } from './api.js';
	import Alert from './Alert.svelte';
	import SelectField from './SelectField.svelte';
	import TextField from './TextField.svelte';
	import { day, money } from './format.svelte.js';
	import { errorText, t } from './i18n.svelte.js';

	let {
		value = $bindable(),
		principal,
		currency,
		previewPath = '/schedule/preview',
		fields = {}
	}: {
		value: PlanValues;
		principal: number | null;
		currency: string;
		previewPath?: string;
		fields?: Record<string, string>;
	} = $props();

	let preview = $state<{ due_on: string; amount: number }[]>([]);
	const err = (k: string) => fields[`schedule.${k}`] ?? '';

	// Ask the server for the exact schedule it would store, a moment after typing stops.
	$effect(() => {
		const body = { principal, schedule: toSchedule(value) };
		if (!body.principal) {
			preview = [];
			return;
		}
		const timer = setTimeout(async () => {
			try {
				preview = (await api<{ installments: { due_on: string; amount: number }[] }>('POST', previewPath, body)).installments;
			} catch {
				preview = [];
			}
		}, 300);
		return () => clearTimeout(timer);
	});
</script>

<p class="md-section-title">{t('repayment')}</p>
<div class="md-segmented" role="group" aria-label={t('repayment')}>
	<button type="button" class="md-state" aria-pressed={value.kind === 'one'} onclick={() => (value.kind = 'one')}>{t('one_payment')}</button>
	<button type="button" class="md-state" aria-pressed={value.kind === 'installments'} onclick={() => (value.kind = 'installments')}>{t('installments')}</button>
</div>

{#if value.kind === 'one'}
	<TextField id="due_date" label={t('due_date')} type="date" bind:value={value.dueDate} error={err('first_due')} required />
{:else}
	<div class="row">
		<TextField id="count" label={t('count')} bind:value={value.count} error={err('count')} inputmode="numeric" required />
		<SelectField id="frequency" label={t('frequency')} bind:value={value.frequency} error={err('frequency')}>
			<option value="monthly">{t('monthly')}</option>
			<option value="weekly">{t('weekly')}</option>
			<option value="every_n_days">{t('every_n_days')}</option>
		</SelectField>
	</div>
	{#if value.frequency === 'every_n_days'}
		<TextField id="every_days" label={t('every_days')} bind:value={value.everyDays} error={err('every_days')} inputmode="numeric" />
	{/if}
	<TextField id="first_due" label={t('first_due')} type="date" bind:value={value.firstDue} error={err('first_due')} required />
{/if}

{#if err('items') || err('mode')}
	<Alert message={errorText(err('items') || err('mode'))} />
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

<style>
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
