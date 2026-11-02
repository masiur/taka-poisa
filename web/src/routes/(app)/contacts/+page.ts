import { api, type ContactSummary } from '#lib/api.js';

export async function load({ fetch, url }) {
	const q = url.searchParams.get('q') ?? '';
	const res = await api<{ contacts: ContactSummary[]; currency: string }>(
		'GET',
		`/contacts?q=${encodeURIComponent(q)}`,
		undefined,
		fetch
	);
	return { ...res, q };
}
