import type { ReminderRule } from './api.js';

const rule = (offset_days: number, recipient: ReminderRule['recipient'], tone: ReminderRule['tone'], repeat?: [number, number]): ReminderRule => ({
	offset_days,
	recipient,
	tone,
	repeat_every_days: repeat?.[0] ?? null,
	repeat_count: repeat?.[1] ?? null,
	send_hour: null,
	paused: false
});

/** Presets from the plan; each rule can be edited afterwards. */
export const presets: Record<'gentle' | 'firm' | 'early', ReminderRule[]> = {
	gentle: [rule(0, 'contact', 'polite'), rule(3, 'contact', 'polite'), rule(7, 'contact', 'polite'), rule(-1, 'self', 'polite')],
	firm: [rule(-3, 'contact', 'polite'), rule(0, 'contact', 'polite'), rule(1, 'contact', 'firm', [3, 5]), rule(1, 'self', 'polite')],
	early: [rule(-7, 'contact', 'polite'), rule(-1, 'contact', 'polite'), rule(0, 'contact', 'polite'), rule(-1, 'self', 'polite')]
};
