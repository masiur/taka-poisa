import { formatDate } from './dates.js';
import { i18n } from './i18n.svelte.js';
import { formatMoney } from './money.js';

// Locale-aware display helpers; they re-render when the language changes.
export const money = (minor: number, currency: string) => formatMoney(minor, currency, i18n.locale);
export const day = (iso: string) => formatDate(iso, i18n.locale);
export const initial = (name: string) => [...name.trim()][0]?.toUpperCase() ?? '?';
