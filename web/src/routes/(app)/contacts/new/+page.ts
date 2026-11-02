import { api } from '#lib/api.js';

export async function load({ fetch }) {
	return api<{ tags: string[] }>('GET', '/contacts/tags', undefined, fetch);
}
