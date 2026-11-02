import { api, type Overview } from '#lib/api.js';

export async function load({ fetch }) {
	return { overview: await api<Overview>('GET', '/overview', undefined, fetch) };
}
