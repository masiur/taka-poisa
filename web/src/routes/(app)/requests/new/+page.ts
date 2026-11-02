import { api, type ContactSummary } from '#lib/api.js';

export async function load({ fetch, url }) {
	const res = await api<{ contacts: ContactSummary[]; currency: string }>('GET', '/contacts', undefined, fetch);
	const contact = Number(url.searchParams.get('contact')) || null;
	return { ...res, contactId: res.contacts.some((c) => c.id === contact && c.email) ? contact : null };
}
