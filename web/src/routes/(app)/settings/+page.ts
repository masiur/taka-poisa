import { api, type AccountSettings } from '#lib/api.js';

export async function load({ fetch }) {
	return { settings: await api<AccountSettings>('GET', '/account', undefined, fetch) };
}
