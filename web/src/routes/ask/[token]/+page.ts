import { api, ApiError, type LinkInfo } from '#lib/api.js';

export async function load({ fetch, params }) {
	try {
		return { info: await api<LinkInfo>('GET', `/public/ask/${params.token}`, undefined, fetch), error: '' };
	} catch (err) {
		return { info: null, error: err instanceof ApiError ? err.code : 'generic' };
	}
}
