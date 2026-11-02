import { api, type AccountSettings, type LoanDetail, type ReminderView } from '#lib/api.js';

export async function load({ fetch, params }) {
	const [loan, rem, account] = await Promise.all([
		api<LoanDetail>('GET', `/agreements/${params.id}`, undefined, fetch),
		api<{ reminders: ReminderView; email_enabled: boolean }>('GET', `/agreements/${params.id}/reminders`, undefined, fetch),
		api<AccountSettings>('GET', '/account', undefined, fetch)
	]);
	return { loan, reminders: rem.reminders, emailEnabled: rem.email_enabled, sendHour: account.send_hour };
}
