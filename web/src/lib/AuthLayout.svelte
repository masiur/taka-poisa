<script lang="ts">
	import type { Snippet } from 'svelte';
	import LanguageSwitch from './LanguageSwitch.svelte';
	import { t } from './i18n.svelte.js';

	let { title, intro = '', children }: { title: string; intro?: string; children: Snippet } = $props();
</script>

<div class="auth">
	<div class="auth__top"><LanguageSwitch /></div>
	<main class="auth__card">
		<p class="auth__brand md-title-medium">{t('app_name')}</p>
		<h1 class="md-headline-small">{title}</h1>
		{#if intro}<p class="md-body-medium md-muted">{intro}</p>{/if}
		<div class="auth__body">{@render children()}</div>
	</main>
</div>

<style>
	/* Google sign-in style: tinted page, white card. */
	.auth {
		min-height: 100dvh;
		background: var(--md-sys-color-surface-container);
		padding: 16px;
		display: grid;
		align-content: start;
		justify-items: center;
		gap: 24px;
	}

	.auth__top {
		justify-self: end;
	}

	.auth__card {
		--md-field-label-bg: var(--md-sys-color-surface-container-lowest);
		width: 100%;
		max-width: 448px;
		padding: 32px 24px;
		border-radius: var(--md-sys-shape-xl);
		background: var(--md-sys-color-surface-container-lowest);
		display: grid;
		gap: 8px;
	}

	.auth__brand {
		color: var(--md-sys-color-primary);
	}

	.auth__body {
		margin-top: 16px;
	}

	@media (min-width: 600px) {
		.auth {
			padding-top: 48px;
		}
		.auth__card {
			padding: 40px;
		}
	}
</style>
