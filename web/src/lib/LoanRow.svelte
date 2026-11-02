<script lang="ts">
	import type { LoanSummary } from './api.js';
	import StatusChip from './StatusChip.svelte';
	import { day, initial, money } from './format.svelte.js';
	import { t } from './i18n.svelte.js';

	let { loan, showContact = true }: { loan: LoanSummary; showContact?: boolean } = $props();

	const title = $derived(
		showContact
			? t(loan.direction === 'lent' ? 'lent_to' : 'borrowed_from', { name: loan.contact_name })
			: (loan.direction === 'lent' ? t('i_lent') : t('i_borrowed')) + ' · ' + money(loan.principal, loan.currency)
	);
</script>

<a class="md-list__item md-state" href="/loans/{loan.id}">
	<span class="md-avatar">{initial(loan.contact_name)}</span>
	<span class="md-list__main">
		<span class="md-body-large">{title}</span>
		<span class="md-body-medium md-muted">
			{#if loan.status === 'active' && loan.next_due_on}
				{t('next_due', { amount: money(loan.next_amount, loan.currency), date: day(loan.next_due_on) })}
			{:else}
				{loan.purpose || day(loan.started_on)}
			{/if}
		</span>
	</span>
	<span class="md-list__trailing">
		{#if loan.status === 'active'}
			<span class="md-title-medium" class:md-money-in={loan.direction === 'lent'}>{money(loan.remaining, loan.currency)}</span>
			{#if loan.overdue > 0}<StatusChip status="overdue" />{/if}
		{:else}
			<StatusChip status={loan.status} />
		{/if}
	</span>
</a>
