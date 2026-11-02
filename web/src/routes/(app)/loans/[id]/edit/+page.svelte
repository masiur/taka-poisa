<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import Icon from '#lib/Icon.svelte';
	import LoanForm, { emptyLoan, type LoanValues } from '#lib/LoanForm.svelte';
	import { emptyPlan } from '#lib/PlanFields.svelte';
	import { t } from '#lib/i18n.svelte.js';
	import { toInput } from '#lib/money.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const loan = $derived(data.loan);

	// Pre-fill from the stored schedule: one installment = one payment, otherwise a
	// monthly plan with the same count and first date. Saving replaces the schedule.
	function initialValues(): LoanValues {
		const items = loan.installments;
		return {
			...emptyLoan(loan.contact_id),
			direction: loan.direction,
			amount: toInput(loan.principal, loan.currency),
			startedOn: loan.started_on,
			purpose: loan.purpose,
			plan: {
				...emptyPlan(),
				kind: items.length === 1 ? 'one' : 'installments',
				dueDate: items[0]?.due_on ?? loan.started_on,
				count: String(items.length),
				firstDue: items[0]?.due_on ?? loan.started_on
			}
		};
	}
</script>

<svelte:head><title>{t('edit_loan')} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/loans/{loan.id}" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t('edit_loan')}</h1>
</header>

<LoanForm
	mode="edit"
	currency={loan.currency}
	initial={initialValues()}
	onsave={async (payload) => {
		await api('PUT', `/agreements/${loan.id}`, payload);
		snackbar.show(t('loan_saved'));
		await goto(`/loans/${loan.id}`, { refreshAll: true });
	}}
/>
