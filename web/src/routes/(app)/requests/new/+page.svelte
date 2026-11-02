<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import Icon from '#lib/Icon.svelte';
	import LoanForm, { emptyLoan } from '#lib/LoanForm.svelte';
	import { t } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
</script>

<svelte:head><title>{t('ask_for_loan')} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t('ask_for_loan')}</h1>
</header>

{#if !data.contacts.some((c) => c.email)}
	<section class="md-card md-card--outlined">
		<p class="md-body-medium">{t('lender_needs_email')}</p>
		<a href="/contacts/new" class="md-button md-button--tonal md-state first">{t('new_contact')}</a>
	</section>
{:else}
	<LoanForm
		mode="request"
		currency={data.currency}
		contacts={data.contacts}
		initial={emptyLoan(data.contactId)}
		onsave={async (payload) => {
			const { id } = await api<{ id: number }>('POST', '/requests', payload);
			snackbar.show(t('request_sent'));
			await goto(`/loans/${id}`);
		}}
	/>
{/if}

<style>
	.first {
		margin-top: 16px;
	}
</style>
