<script lang="ts">
	import { goto } from '$app/navigation';
	import Fab from '#lib/Fab.svelte';
	import Icon from '#lib/Icon.svelte';
	import { initial, money } from '#lib/format.svelte.js';
	import { t } from '#lib/i18n.svelte.js';

	let { data } = $props();

	// svelte-ignore state_referenced_locally
	let q = $state(data.q);
	let timer: ReturnType<typeof setTimeout>;

	function search() {
		clearTimeout(timer);
		timer = setTimeout(() => {
			goto(q ? `?q=${encodeURIComponent(q)}` : '?', { replace: true, reset: false });
		}, 250);
	}
</script>

<svelte:head><title>{t('contacts')} · {t('app_name')}</title></svelte:head>

<h1 class="md-headline-small">{t('contacts')}</h1>

<label class="search md-state">
	<Icon name="search" />
	<input type="search" bind:value={q} oninput={search} placeholder={t('search_contacts')} aria-label={t('search_contacts')} />
</label>

{#if data.contacts.length === 0}
	<p class="md-body-medium md-muted empty">{data.q ? t('err_not_found') : t('no_contacts')}</p>
{:else}
	<ul class="md-list">
		{#each data.contacts as c (c.id)}
			<li>
				<a class="md-list__item md-state" href="/contacts/{c.id}">
					<span class="md-avatar">{initial(c.name)}</span>
					<span class="md-list__main">
						<span class="md-body-large">{c.name}</span>
						<span class="md-body-medium md-muted">{c.phone || c.email || '—'}</span>
					</span>
					<span class="md-list__trailing md-body-medium">
						{#if c.receivable > 0}
							<span class="md-money-in">{t('they_owe_you', { amount: money(c.receivable, data.currency) })}</span>
						{/if}
						{#if c.payable > 0}
							<span>{t('you_owe_them', { amount: money(c.payable, data.currency) })}</span>
						{/if}
						{#if c.receivable === 0 && c.payable === 0}
							<span class="md-muted">{t('settled_up')}</span>
						{/if}
					</span>
				</a>
			</li>
		{/each}
	</ul>
{/if}

<Fab href="/contacts/new" label={t('new_contact')} icon="person" />

<style>
	/* Gmail-style search bar: filled pill. */
	.search {
		display: flex;
		align-items: center;
		gap: 12px;
		height: 48px;
		margin: 16px 0 8px;
		padding: 0 16px;
		border-radius: var(--md-sys-shape-full);
		background: var(--md-sys-color-surface-container-high);
		color: var(--md-sys-color-on-surface-variant);
	}

	.search input {
		flex: 1;
		min-width: 0;
		height: 100%;
		border: 0;
		background: transparent;
		color: var(--md-sys-color-on-surface);
		font: 400 1rem/1.5rem var(--md-sys-typeface);
	}

	.search input:focus {
		outline: none;
	}

	.search:focus-within {
		background: var(--md-sys-color-surface);
		box-shadow: var(--md-sys-elevation-1);
	}

	.empty {
		padding: 24px 0;
	}
</style>
