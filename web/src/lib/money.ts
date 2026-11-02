// Amounts travel as integers in the currency's minor unit (poisa for BDT).

const bnDigits = '০১২৩৪৫৬৭৮৯';

/** Decimal places of a currency's minor unit (2 for BDT, 0 for JPY). */
export function fractionDigits(currency: string): number {
	return new Intl.NumberFormat('en', { style: 'currency', currency }).resolvedOptions().maximumFractionDigits ?? 2;
}

/**
 * Parses what a person typed ("30,000", "১০০০০.৫০") into minor units, or null if it
 * isn't a positive amount with at most the currency's decimal places. No floating point.
 */
export function toMinor(input: string, currency: string): number | null {
	const digits = fractionDigits(currency);
	const ascii = input
		.trim()
		.replace(/[০-৯]/g, (c) => String(bnDigits.indexOf(c)))
		.replace(/[,\s]/g, '');
	const m = new RegExp(`^(\\d+)(?:\\.(\\d{0,${digits}}))?$`).exec(ascii);
	if (!m) return null;
	const value = Number(m[1]) * 10 ** digits + Number((m[2] ?? '').padEnd(digits, '0') || 0);
	return Number.isSafeInteger(value) && value > 0 ? value : null;
}

/** Minor units back to a plain editable string ("30000.5" → "30000.50"). */
export function toInput(minor: number, currency: string): string {
	const digits = fractionDigits(currency);
	if (digits === 0) return String(minor);
	const whole = Math.trunc(minor / 10 ** digits);
	const frac = minor % 10 ** digits;
	return frac ? `${whole}.${String(frac).padStart(digits, '0')}` : String(whole);
}

/** Formats minor units for display, in Bangla or English digits; decimals only when present. */
export function formatMoney(minor: number, currency: string, locale: 'bn' | 'en'): string {
	const digits = fractionDigits(currency);
	const value = minor / 10 ** digits;
	return new Intl.NumberFormat(locale === 'bn' ? 'bn-BD' : 'en-IN', {
		style: 'currency',
		currency,
		minimumFractionDigits: minor % 10 ** digits ? digits : 0,
		maximumFractionDigits: digits
	}).format(value);
}
