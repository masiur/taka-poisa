// Dates are calendar days ("2026-11-02") with no time zone; format them as UTC so they never shift.

export function formatDate(iso: string, locale: 'bn' | 'en'): string {
	return new Intl.DateTimeFormat(locale === 'bn' ? 'bn-BD' : 'en-GB', {
		day: 'numeric',
		month: 'short',
		year: 'numeric',
		timeZone: 'UTC'
	}).format(new Date(`${iso}T00:00:00Z`));
}

/** Today's date on this device, as YYYY-MM-DD. */
export function todayISO(): string {
	const d = new Date();
	return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export function addMonthsISO(iso: string, n: number): string {
	const d = new Date(`${iso}T00:00:00Z`);
	const day = d.getUTCDate();
	d.setUTCDate(1);
	d.setUTCMonth(d.getUTCMonth() + n);
	const last = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate();
	d.setUTCDate(Math.min(day, last));
	return d.toISOString().slice(0, 10);
}
