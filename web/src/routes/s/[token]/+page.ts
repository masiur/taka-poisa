import { api, ApiError, type Statement } from '#lib/api.js';

export async function load({ fetch, params }) {
	try {
		return { statement: await api<Statement>('GET', `/public/statement/${params.token}`, undefined, fetch), error: '' };
	} catch (err) {
		return { statement: null, error: err instanceof ApiError ? err.code : 'generic' };
	}
}
