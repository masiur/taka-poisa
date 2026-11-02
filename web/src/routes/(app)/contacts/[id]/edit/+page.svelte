<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import ContactForm from '#lib/ContactForm.svelte';
	import Icon from '#lib/Icon.svelte';
	import { t } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';

	let { data } = $props();
	const c = $derived(data.contact);
</script>

<svelte:head><title>{t('edit')} · {c.name} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/contacts/{c.id}" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t('edit')}</h1>
</header>

<ContactForm
	initial={{ name: c.name, email: c.email, phone: c.phone, locale: c.locale, notes: c.notes, tags: c.tags }}
	tags={data.tags}
	onsave={async (v) => {
		await api('PUT', `/contacts/${c.id}`, v);
		snackbar.show(t('contact_saved'));
		await goto(`/contacts/${c.id}`, { refreshAll: true });
	}}
/>
