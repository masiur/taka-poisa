import { api, type AccountSettings, type ReminderRule, type RequestLink } from '#lib/api.js';

export async function load({ fetch }) {
	const [settings, defaults, links] = await Promise.all([
		api<AccountSettings>('GET', '/account', undefined, fetch),
		api<{ rules: ReminderRule[] }>('GET', '/account/reminder-defaults', undefined, fetch),
		api<{ links: RequestLink[] }>('GET', '/request-links', undefined, fetch)
	]);
	return { settings, defaultRules: defaults.rules, links: links.links };
}
