import { api, type LoanSummary, type Overview, type RequestView } from '#lib/api.js';

export async function load({ fetch }) {
	const [overview, requested, approved, incoming] = await Promise.all([
		api<Overview>('GET', '/overview', undefined, fetch),
		api<{ agreements: LoanSummary[] }>('GET', '/agreements?status=requested', undefined, fetch),
		api<{ agreements: LoanSummary[] }>('GET', '/agreements?status=approved', undefined, fetch),
		api<{ requests: RequestView[] }>('GET', '/requests/incoming', undefined, fetch)
	]);
	return {
		overview,
		// People asking me to lend, through my links.
		linkRequests: [...requested.agreements, ...approved.agreements].filter((a) => a.origin === 'link'),
		// My own requests to others.
		sentRequests: requested.agreements.filter((a) => a.origin === 'request'),
		incoming: incoming.requests
	};
}
