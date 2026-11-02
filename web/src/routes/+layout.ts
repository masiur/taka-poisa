import { redirect } from '@sveltejs/kit';
import { api, ApiError, type Me } from '#lib/api.js';

export const ssr = false;

// Route guard: first run → /setup; signed out → /login; signed in → never /login or /setup.
// The query string is kept so ?error= from the Google callback survives the redirect.
export async function load({ fetch, url }): Promise<{ me: Me | null; google: boolean }> {
	// Statement (/s/), request (/r/) and ask (/ask/) pages are for people without an account: no guard.
	if (['/s/', '/r/', '/ask/'].some((p) => url.pathname.startsWith(p))) return { me: null, google: false };
	const { needed, google } = await api<{ needed: boolean; google: boolean }>('GET', '/setup', undefined, fetch);
	if (needed) {
		if (url.pathname !== '/setup') redirect(307, '/setup' + url.search);
		return { me: null, google };
	}
	let me: Me;
	try {
		me = await api<Me>('GET', '/me', undefined, fetch);
	} catch (e) {
		if (e instanceof ApiError && e.status === 401) {
			if (url.pathname !== '/login') redirect(307, '/login' + url.search);
			return { me: null, google };
		}
		throw e;
	}
	if (url.pathname === '/login' || url.pathname === '/setup') redirect(307, '/');
	return { me, google };
}
