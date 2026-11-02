import { api, ApiError, type RequestView } from '#lib/api.js';

export async function load({ fetch, params }) {
	try {
		return { request: await api<RequestView>('GET', `/public/request/${params.token}`, undefined, fetch), error: '' };
	} catch (err) {
		return { request: null, error: err instanceof ApiError ? err.code : 'generic' };
	}
}
