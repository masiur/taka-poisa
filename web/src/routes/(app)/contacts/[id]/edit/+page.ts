import { api, type Contact } from '#lib/api.js';

export async function load({ fetch, params }) {
	const { contact } = await api<{ contact: Contact }>('GET', `/contacts/${params.id}`, undefined, fetch);
	return { contact };
}
