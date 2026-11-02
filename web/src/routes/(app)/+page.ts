import { api, type LoanSummary, type Overview, type RequestView } from '#lib/api.js';

export async function load({ fetch }) {
	const [overview, sent, incoming] = await Promise.all([
		api<Overview>('GET', '/overview', undefined, fetch),
		api<{ agreements: LoanSummary[] }>('GET', '/agreements?status=requested', undefined, fetch),
		api<{ requests: RequestView[] }>('GET', '/requests/incoming', undefined, fetch)
	]);
	return { overview, sentRequests: sent.agreements, incoming: incoming.requests };
}
