<script lang="ts">
	import { refreshAll } from '$app/navigation';
	import { api, ApiError, type RequestLink } from './api.js';
	import Alert from './Alert.svelte';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';
	import TextField from './TextField.svelte';
	import { money, num } from './format.svelte.js';
	import { errorText, t } from './i18n.svelte.js';
	import { toMinor } from './money.js';
	import { snackbar } from './snackbar.svelte.js';

	let { links, currency }: { links: RequestLink[]; currency: string } = $props();

	let createOpen = $state(false);
	let label = $state('');
	let max = $state('');
	let fields = $state<Record<string, string>>({});
	let error = $state('');
	let revoking = $state<RequestLink | null>(null);
	let revokeOpen = $state(false);

	async function copy(l: RequestLink) {
		await navigator.clipboard.writeText(l.url);
		snackbar.show(t('copied'));
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		fields = {};
		error = '';
		const maxAmount = max.trim() ? toMinor(max, currency) : null;
		if (max.trim() && !maxAmount) {
			fields = { max_amount: 'invalid' };
			return;
		}
		try {
			const l = await api<RequestLink>('POST', '/request-links', { label, max_amount: maxAmount });
			createOpen = false;
			await navigator.clipboard.writeText(l.url).catch(() => {});
			snackbar.show(t('copied'));
			await refreshAll();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation') fields = err.fields;
			else error = errorText(err instanceof ApiError ? err.code : 'generic');
		}
	}

	async function revoke() {
		if (!revoking) return;
		await api('DELETE', `/request-links/${revoking.id}`).catch(() => {});
		revokeOpen = false;
		snackbar.show(t('link_revoked'));
		await refreshAll();
	}
</script>

{#if links.length === 0}
	<p class="md-body-medium md-muted">{t('no_links')}</p>
{:else}
	<ul class="md-list">
		{#each links as l (l.id)}
			<li class="md-list__item">
				<Icon name="link" />
				<span class="md-list__main">
					<span class="md-body-large">{l.contact_name || l.label || t('link_for_anyone')}</span>
					<span class="md-body-small md-muted">
						{l.url.replace(/^https?:\/\//, '')}
						{#if l.max_amount} · {t('max_amount_hint', { amount: money(l.max_amount, currency) })}{/if}
						{#if l.open_count} · {t('link_open', { n: num(l.open_count) })}{/if}
					</span>
				</span>
				<button type="button" class="md-icon-button md-state" title={t('copy_link')} aria-label={t('copy_link')} onclick={() => copy(l)}><Icon name="content_copy" /></button>
				<button type="button" class="md-icon-button md-state" title={t('revoke')} aria-label={t('revoke')} onclick={() => { revoking = l; revokeOpen = true; }}><Icon name="delete" /></button>
			</li>
		{/each}
	</ul>
{/if}
<button type="button" class="md-button md-button--outlined md-state new" onclick={() => { label = ''; max = ''; fields = {}; error = ''; createOpen = true; }}>
	<Icon name="add" size={18} />{t('new_link')}
</button>

<Dialog bind:open={createOpen} title={t('new_link')}>
	<form id="link_form" class="md-form" onsubmit={create} novalidate>
		<TextField id="link_label" label={t('link_label')} hint={t('link_label_hint')} bind:value={label} error={fields.label} maxlength={100} />
		<TextField id="link_max" label="{t('link_max')} ({currency})" bind:value={max} error={fields.max_amount} inputmode="decimal" />
		{#if error}<Alert message={error} />{/if}
	</form>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (createOpen = false)}>{t('cancel')}</button>
		<button type="submit" form="link_form" class="md-button md-button--filled md-state">{t('new_link')}</button>
	{/snippet}
</Dialog>

<Dialog bind:open={revokeOpen} title={t('revoke_link')}>
	<p class="md-body-medium">{t('revoke_link_body')}</p>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (revokeOpen = false)}>{t('cancel')}</button>
		<button type="button" class="md-button md-button--filled md-state" onclick={revoke}>{t('revoke')}</button>
	{/snippet}
</Dialog>

<style>
	.new {
		justify-self: start;
		margin-top: 8px;
	}
</style>
