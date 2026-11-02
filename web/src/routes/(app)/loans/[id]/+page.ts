import { api, type LoanDetail } from '#lib/api.js';

export async function load({ fetch, params }) {
	return { loan: await api<LoanDetail>('GET', `/agreements/${params.id}`, undefined, fetch) };
}
