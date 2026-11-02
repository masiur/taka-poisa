import { api, type AccountSettings, type ReminderRule } from '#lib/api.js';

export async function load({ fetch }) {
	const [settings, defaults] = await Promise.all([
		api<AccountSettings>('GET', '/account', undefined, fetch),
		api<{ rules: ReminderRule[] }>('GET', '/account/reminder-defaults', undefined, fetch)
	]);
	return { settings, defaultRules: defaults.rules };
}
