import { api, type Contact, type LoanSummary } from '#lib/api.js';

export async function load({ fetch, params }) {
	return await api<{ contact: Contact; agreements: LoanSummary[]; currency: string }>(
		'GET',
		`/contacts/${params.id}`,
		undefined,
		fetch
	);
}
