import { api, type ContactSummary } from '#lib/api.js';

export async function load({ fetch, url }) {
	const q = url.searchParams.get('q') ?? '';
	const tag = url.searchParams.get('tag') ?? '';
	const [res, { tags }] = await Promise.all([
		api<{ contacts: ContactSummary[]; currency: string }>(
			'GET',
			`/contacts?${new URLSearchParams({ q, tag })}`,
			undefined,
			fetch
		),
		api<{ tags: string[] }>('GET', '/contacts/tags', undefined, fetch)
	]);
	return { ...res, tags, q, tag };
}
