<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.js';
	import Icon, { type IconName } from '#lib/Icon.svelte';
	import { t, type Key } from '#lib/i18n.svelte.js';

	let { children } = $props();

	const destinations: { href: string; icon: IconName; activeIcon: IconName; label: Key }[] = [
		{ href: '/', icon: 'home', activeIcon: 'home-fill', label: 'home' },
		{ href: '/settings', icon: 'settings', activeIcon: 'settings-fill', label: 'settings' }
	];

	async function logout() {
		await api('POST', '/logout').catch(() => {});
		await goto('/login', { invalidateAll: true });
	}
</script>

<div class="shell">
	<!-- M3 navigation: bottom navigation bar on phones, navigation rail from 600px. -->
	<nav class="nav" aria-label={t('app_name')}>
		{#each destinations as d (d.href)}
			{@const active = page.url.pathname === d.href}
			<a href={d.href} class="nav__item" aria-current={active ? 'page' : undefined}>
				<span class="nav__indicator md-state"><Icon name={active ? d.activeIcon : d.icon} /></span>
				<span class="nav__label md-label-medium">{t(d.label)}</span>
			</a>
		{/each}
	</nav>

	<div class="shell__content">
		<header class="top_bar">
			<span class="md-title-large top_bar__title">{t('app_name')}</span>
			<button type="button" class="md-icon-button md-state" onclick={logout} aria-label={t('logout')} title={t('logout')}>
				<Icon name="logout" />
			</button>
		</header>
		<main class="page">
			{@render children()}
		</main>
	</div>
</div>

<style>
	.shell {
		--nav-bar-height: 80px;
		min-height: 100dvh;
	}

	.shell__content {
		padding-bottom: calc(var(--nav-bar-height) + env(safe-area-inset-bottom));
	}

	.top_bar {
		position: sticky;
		top: 0;
		z-index: 5;
		display: flex;
		align-items: center;
		justify-content: space-between;
		height: 64px;
		padding: 0 4px 0 16px;
		background: var(--md-sys-color-surface-container-low);
	}

	.top_bar__title {
		color: var(--md-sys-color-on-surface);
	}

	/* Content on a white rounded panel over the tinted page, like Gmail's message list. */
	.page {
		--md-field-label-bg: var(--md-sys-color-surface);
		min-height: calc(100dvh - 64px - var(--nav-bar-height) - env(safe-area-inset-bottom));
		padding: 24px 16px;
		background: var(--md-sys-color-surface);
		border-radius: var(--md-sys-shape-lg) var(--md-sys-shape-lg) 0 0;
	}

	/* Navigation bar (compact) */
	.nav {
		position: fixed;
		inset: auto 0 0 0;
		z-index: 10;
		display: flex;
		justify-content: space-around;
		height: calc(var(--nav-bar-height) + env(safe-area-inset-bottom));
		padding: 12px 0 calc(16px + env(safe-area-inset-bottom));
		background: var(--md-sys-color-surface-container);
	}

	.nav__item {
		flex: 1;
		max-width: 120px;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		text-decoration: none;
		color: var(--md-sys-color-on-surface-variant);
	}

	.nav__indicator {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 64px;
		height: 32px;
		border-radius: var(--md-sys-shape-full);
		color: var(--md-sys-color-on-surface-variant);
	}

	/* Gmail's selected item: light blue pill, bold label. */
	.nav__item[aria-current='page'] .nav__indicator {
		background: var(--md-sys-color-primary-container);
		color: var(--md-sys-color-on-primary-container);
	}

	.nav__item[aria-current='page'] .nav__label {
		color: var(--md-sys-color-on-surface);
		font-weight: 700;
	}

	.nav__item:focus-visible {
		outline: none;
	}

	.nav__item:focus-visible .nav__indicator {
		outline: 3px solid var(--md-sys-color-secondary);
		outline-offset: 2px;
	}

	/* The indicator carries the hover/press state layer for the whole destination. */
	.nav__item:hover .nav__indicator::before {
		opacity: 0.08;
	}

	/* Navigation rail (medium and up) */
	@media (min-width: 600px) {
		.shell {
			--nav-bar-height: 0px;
		}

		.nav {
			inset: 0 auto 0 0;
			width: 80px;
			height: auto;
			flex-direction: column;
			justify-content: flex-start;
			gap: 12px;
			padding: 88px 0 0;
			background: var(--md-sys-color-surface-container-low);
		}

		.nav__item {
			flex: none;
		}

		.nav__indicator {
			width: 56px;
		}

		.shell__content {
			margin-left: 80px;
			padding: 0 16px 16px 0;
		}

		.page {
			min-height: calc(100dvh - 64px - 16px);
			padding: 24px 32px;
			border-radius: var(--md-sys-shape-lg);
		}
	}
</style>
