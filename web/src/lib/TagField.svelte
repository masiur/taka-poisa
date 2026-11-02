<script lang="ts">
	import Icon from './Icon.svelte';
	import TextField from './TextField.svelte';
	import { t } from './i18n.svelte.js';

	// Tags group contacts (Family, Colleague, ...). Enter or comma adds what is typed; the
	// suggestion chips offer the account's existing tags plus a few presets.
	let {
		value = $bindable([]),
		suggestions = [],
		error = ''
	}: { value?: string[]; suggestions?: string[]; error?: string } = $props();

	const MAX = 10;
	let draft = $state('');

	const has = (tag: string) => value.some((v) => v.toLowerCase() === tag.toLowerCase());
	const presets = $derived([t('tag_family'), t('tag_friend'), t('tag_colleague'), t('tag_business')]);
	const offered = $derived(
		[...new Set([...suggestions, ...presets])].filter((s) => !has(s)).slice(0, 8)
	);

	function add(raw: string) {
		const tag = raw.split(/\s+/).filter(Boolean).join(' ');
		if (tag && !has(tag) && value.length < MAX) value = [...value, tag];
	}

	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',') {
			e.preventDefault();
			add(draft);
			draft = '';
		} else if (e.key === 'Backspace' && draft === '' && value.length > 0) {
			value = value.slice(0, -1);
		}
	}
</script>

<div class="tag_field">
	{#if value.length > 0}
		<ul class="tag_field__chips" aria-label={t('tags')}>
			{#each value as tag (tag)}
				<li class="md-chip md-chip--neutral tag_field__chip">
					{tag}
					<button type="button" class="tag_field__remove" aria-label={t('remove_tag', { tag })} onclick={() => (value = value.filter((v) => v !== tag))}>
						<Icon name="close" size={16} />
					</button>
				</li>
			{/each}
		</ul>
	{/if}
	{#if value.length < MAX}
		<TextField
			id="tags"
			label={t('tags')}
			hint={t('tags_hint')}
			bind:value={draft}
			{error}
			{onkeydown}
			onblur={() => {
				add(draft);
				draft = '';
			}}
			maxlength={40}
			autocomplete="off"
		/>
		{#if offered.length > 0}
			<div class="tag_field__suggestions">
				{#each offered as s (s)}
					<button type="button" class="md-chip-button md-state" onclick={() => add(s)}>+ {s}</button>
				{/each}
			</div>
		{/if}
	{/if}
</div>

<style>
	.tag_field {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.tag_field__chips,
	.tag_field__suggestions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.tag_field__chip {
		height: 32px;
		padding-right: 4px;
		gap: 4px;
		font-size: 0.875rem;
	}

	.tag_field__remove {
		display: inline-flex;
		padding: 4px;
		border: 0;
		border-radius: var(--md-sys-shape-full);
		background: transparent;
		color: inherit;
		cursor: pointer;
	}
</style>
