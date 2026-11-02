<script lang="ts">
	import { page } from '$app/state';
	import { refreshAll } from '$app/navigation';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.js';
	import Alert from '#lib/Alert.svelte';
	import Dialog from '#lib/Dialog.svelte';
	import Icon from '#lib/Icon.svelte';
	import LanguageSwitch from '#lib/LanguageSwitch.svelte';
	import TextField from '#lib/TextField.svelte';
	import { day, money } from '#lib/format.svelte.js';
	import { errorText, i18n, t } from '#lib/i18n.svelte.js';

	let { data } = $props();
	const rq = $derived(data.request);
	const token = $derived(page.params.token);

	let note = $state('');
	let busy = $state(false);
	let error = $state('');
	let done = $state<'' | 'accepted' | 'declined' | 'blocked'>('');
	let blockOpen = $state(false);

	onMount(() => {
		let chosen = false;
		try {
			chosen = !!localStorage.getItem('locale');
		} catch {
			// storage unavailable
		}
		if (rq && !chosen) i18n.locale = rq.locale;
		if (page.url.searchParams.get('block') === '1' && rq?.status === 'requested') blockOpen = true;
	});

	async function answer(accept: boolean) {
		busy = true;
		error = '';
		try {
			await api('POST', `/public/request/${token}/${accept ? 'accept' : 'decline'}`, { note });
			done = accept ? 'accepted' : 'declined';
			await refreshAll();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'generic');
		} finally {
			busy = false;
		}
	}

	async function block() {
		try {
			await api('POST', `/public/request/${token}/block`);
			blockOpen = false;
			done = 'blocked';
			await refreshAll();
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'generic');
		}
	}
</script>

<svelte:head>
	<title>{rq ? t('rq_title', { name: rq.borrower_name, amount: money(rq.principal, rq.currency) }) : t('app_name')}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="request">
	<div class="top"><LanguageSwitch /></div>
	<main class="card">
		<p class="brand md-title-medium">{t('app_name')}</p>
		{#if !rq}
			<Alert message={errorText(data.error)} />
		{:else}
			<h1 class="md-headline-small">{t('rq_title', { name: rq.borrower_name, amount: money(rq.principal, rq.currency) })}</h1>
			<p class="md-body-medium md-muted">{t('rq_intro', { name: rq.borrower_name })}</p>

			{#if rq.message}
				<section class="message">
					<p class="md-label-large md-muted">{t('rq_message')}</p>
					<p class="md-body-large">“{rq.message}”</p>
				</section>
			{/if}
			{#if rq.purpose}<p class="md-body-medium">{t('purpose')}: {rq.purpose}</p>{/if}

			<p class="md-label-large md-muted">{t('rq_plan')}</p>
			<ul class="md-list">
				{#each rq.installments as it, i (i)}
					<li class="md-list__item row">
						<span class="md-list__main md-body-large">{day(it.due_on)}</span>
						<span class="md-title-medium">{money(it.amount, rq.currency)}</span>
					</li>
				{/each}
			</ul>

			{#if done === 'accepted'}
				<p class="md-note"><Icon name="check" size={20} />{t('rq_accepted', { name: rq.borrower_name })}</p>
			{:else if done === 'declined'}
				<p class="md-note">{t('rq_declined', { name: rq.borrower_name })}</p>
			{:else if done === 'blocked'}
				<p class="md-note"><Icon name="notifications_off" size={20} />{t('rq_blocked', { name: rq.borrower_name })}</p>
			{:else if rq.status !== 'requested'}
				<p class="md-note">{t('rq_closed')}</p>
			{:else}
				<p class="md-body-medium">{t('rq_until', { date: day(rq.expires_at.slice(0, 10)) })}</p>
				<TextField id="note" label={t('rq_note', { name: rq.borrower_name })} multiline bind:value={note} maxlength={500} />
				{#if error}<Alert message={error} />{/if}
				<div class="answers">
					<button type="button" class="md-button md-button--outlined md-state" disabled={busy} onclick={() => answer(false)}>{t('decline')}</button>
					<button type="button" class="md-button md-button--filled md-state" disabled={busy} onclick={() => answer(true)}>{t('accept')}</button>
				</div>
				<button type="button" class="md-button md-button--text md-state block" onclick={() => (blockOpen = true)}>
					<Icon name="notifications_off" size={18} />{t('rq_block', { name: rq.borrower_name })}
				</button>
			{/if}
		{/if}
	</main>
</div>

{#if rq}
	<Dialog bind:open={blockOpen} title={t('block')}>
		<p class="md-body-medium">{t('rq_block_confirm', { name: rq.borrower_name })}</p>
		{#snippet actions()}
			<button type="button" class="md-button md-button--text md-state" onclick={() => (blockOpen = false)}>{t('cancel')}</button>
			<button type="button" class="md-button md-button--filled md-state" onclick={block}>{t('block')}</button>
		{/snippet}
	</Dialog>
{/if}

<style>
	.request {
		min-height: 100dvh;
		padding: 16px;
		display: grid;
		align-content: start;
		justify-items: center;
		gap: 16px;
		background: var(--md-sys-color-surface-container);
	}

	.top {
		justify-self: end;
	}

	.card {
		--md-field-label-bg: var(--md-sys-color-surface-container-lowest);
		width: 100%;
		max-width: 520px;
		padding: 28px 20px;
		border-radius: var(--md-sys-shape-xl);
		background: var(--md-sys-color-surface-container-lowest);
		display: grid;
		gap: 12px;
	}

	.brand {
		color: var(--md-sys-color-primary);
	}

	.message {
		padding: 16px;
		border-radius: var(--md-sys-shape-lg);
		background: var(--md-sys-color-surface-container-low);
	}

	.row {
		min-height: 48px;
		padding: 4px 0;
	}

	.answers {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
	}

	.block {
		justify-self: start;
		color: var(--md-sys-color-on-surface-variant);
	}

	@media (min-width: 600px) {
		.request {
			padding-top: 40px;
		}
		.card {
			padding: 36px;
		}
	}
</style>
