import { api, type Contact } from '#lib/api.js';

export async function load({ fetch, params }) {
	const [{ contact }, { tags }] = await Promise.all([
		api<{ contact: Contact }>('GET', `/contacts/${params.id}`, undefined, fetch),
		api<{ tags: string[] }>('GET', '/contacts/tags', undefined, fetch)
	]);
	return { contact, tags };
}
