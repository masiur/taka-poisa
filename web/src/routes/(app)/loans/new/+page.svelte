<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import Icon from '#lib/Icon.svelte';
	import LoanForm, { emptyLoan } from '#lib/LoanForm.svelte';
	import { t } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const back = $derived(data.contactId ? `/contacts/${data.contactId}` : '/');
</script>

<svelte:head><title>{t('new_loan')} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href={back} class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t('new_loan')}</h1>
</header>

{#if data.contacts.length === 0}
	<section class="md-card md-card--outlined">
		<p class="md-body-medium">{t('add_contact_first')}</p>
		<a href="/contacts/new" class="md-button md-button--tonal md-state first_contact">{t('new_contact')}</a>
	</section>
{:else}
	<LoanForm
		mode="create"
		currency={data.currency}
		contacts={data.contacts}
		initial={emptyLoan(data.contactId)}
		onsave={async (payload) => {
			const { id } = await api<{ id: number }>('POST', '/agreements', payload);
			snackbar.show(t('loan_saved'));
			await goto(`/loans/${id}`);
		}}
	/>
{/if}

<style>
	.first_contact {
		margin-top: 16px;
	}
</style>
