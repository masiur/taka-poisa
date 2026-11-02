<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '#lib/api.js';
	import ContactForm from '#lib/ContactForm.svelte';
	import Icon from '#lib/Icon.svelte';
	import { i18n, t } from '#lib/i18n.svelte.js';
	import { snackbar } from '#lib/snackbar.svelte.js';
</script>

<svelte:head><title>{t('new_contact')} · {t('app_name')}</title></svelte:head>

<header class="md-page-header">
	<a href="/contacts" class="md-icon-button md-state" aria-label={t('back')}><Icon name="arrow_back" /></a>
	<h1 class="md-headline-small">{t('new_contact')}</h1>
</header>

<ContactForm
	initial={{ name: '', email: '', phone: '', locale: i18n.locale, notes: '' }}
	onsave={async (v) => {
		const { id } = await api<{ id: number }>('POST', '/contacts', v);
		snackbar.show(t('contact_saved'));
		await goto(`/contacts/${id}`);
	}}
/>
