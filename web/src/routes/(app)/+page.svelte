<script lang="ts">
	import { t } from '#lib/i18n.svelte.js';

	let { data } = $props();
</script>

<svelte:head><title>{t('app_name')}</title></svelte:head>

<h1 class="md-headline-small">{t('welcome', { name: data.me?.user.name ?? '' })}</h1>
<section class="md-card md-card--outlined empty">
	<p class="md-body-large md-muted">{t('home_empty')}</p>
</section>

<style>
	.empty {
		margin-top: 24px;
		padding: 48px 24px;
		text-align: center;
	}
</style>
