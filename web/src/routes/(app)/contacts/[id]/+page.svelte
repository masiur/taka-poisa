<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Dialog from '#lib/Dialog.svelte';
	import Fab from '#lib/Fab.svelte';
	import Icon from '#lib/Icon.svelte';
	import LoanRow from '#lib/LoanRow.svelte';
	import { initial, money } from '#lib/format.svelte.js';
	import { errorText, t } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const c = $derived(data.contact);
	const open = $derived(data.agreements.filter((a) => a.status === 'active'));
	const closed = $derived(data.agreements.filter((a) => a.status !== 'active'));
	const receivable = $derived(open.filter((a) => a.direction === 'lent').reduce((s, a) => s + a.remaining, 0));
	const payable = $derived(open.filter((a) => a.direction === 'borrowed').reduce((s, a) => s + a.remaining, 0));

	let confirmDelete = $state(false);
	let deleteError = $state('');

	async function remove() {
		deleteError = '';
		try {
			await api('DELETE', `/contacts/${c.id}`);
			confirmDelete = false;
			snackbar.show(t('contact_deleted'));
			await goto('/contacts');
		} catch (err) {
			deleteError = errorText(err instanceof ApiError ? err.code : 'generic');
		}
	}
</script>

<svelte:head><title>{c.name} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/contacts" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{c.name}</h1>
	<a href="/contacts/{c.id}/edit" class="md-icon-button md-state" aria-label={t('edit')} title={t('edit')}><Icon name="edit" /></a>
	<button type="button" class="md-icon-button md-state" aria-label={t('delete')} title={t('delete')} onclick={() => (confirmDelete = true)}>
		<Icon name="delete" />
	</button>
</header>

<section class="profile">
	<span class="md-avatar profile__avatar">{initial(c.name)}</span>
	<div class="profile__lines md-body-medium">
		{#if c.phone}<a href="tel:{c.phone}" class="profile__line"><Icon name="call" size={18} />{c.phone}</a>{/if}
		{#if c.email}<a href="mailto:{c.email}" class="profile__line"><Icon name="mail" size={18} />{c.email}</a>{/if}
		{#if c.notes}<p class="md-muted notes">{c.notes}</p>{/if}
	</div>
</section>

<div class="balances">
	<div class="md-card md-card--outlined">
		<p class="md-label-large md-muted">{t('you_will_get')}</p>
		<p class="md-title-large md-money-in">{money(receivable, data.currency)}</p>
	</div>
	<div class="md-card md-card--outlined">
		<p class="md-label-large md-muted">{t('you_owe')}</p>
		<p class="md-title-large">{money(payable, data.currency)}</p>
	</div>
</div>

<h2 class="md-section-title">{t('loans')}</h2>
{#if data.agreements.length === 0}
	<p class="md-body-medium md-muted">{t('no_loans')}</p>
{:else}
	<ul class="md-list">
		{#each [...open, ...closed] as loan (loan.id)}
			<li><LoanRow {loan} showContact={false} /></li>
		{/each}
	</ul>
{/if}

<Fab href="/loans/new?contact={c.id}" label={t('new_loan')} />

<Dialog bind:open={confirmDelete} title={t('delete_contact')}>
	<p class="md-body-medium">{t('delete_contact_body', { name: c.name })}</p>
	{#if deleteError}<Alert message={deleteError} />{/if}
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (confirmDelete = false)}>{t('cancel')}</button>
		<button type="button" class="md-button md-button--text md-state danger" onclick={remove}>{t('delete')}</button>
	{/snippet}
</Dialog>

<style>
	.profile {
		display: flex;
		gap: 16px;
		align-items: flex-start;
	}

	.profile__avatar {
		width: 56px;
		height: 56px;
		font-size: 1.5rem;
	}

	.profile__lines {
		display: grid;
		gap: 4px;
		min-width: 0;
	}

	.profile__line {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		min-height: 32px;
		color: var(--md-sys-color-on-surface-variant);
		text-decoration: none;
		overflow-wrap: anywhere;
	}

	.notes {
		white-space: pre-line;
	}

	.balances {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
		margin-top: 24px;
	}

	.danger {
		color: var(--md-sys-color-error);
	}
</style>
