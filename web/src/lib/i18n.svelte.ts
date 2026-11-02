const en = {
	app_name: 'Taka Poisa',
	setup_title: 'Set up Taka Poisa',
	setup_intro: 'Create the owner account for this server.',
	your_name: 'Your name',
	email: 'Email',
	password: 'Password',
	password_hint: 'At least 8 characters',
	book_name: 'Ledger name',
	book_name_hint: 'For example: My ledger, Rahim Store',
	language: 'Language',
	create_account: 'Create account',
	login_title: 'Log in',
	login: 'Log in',
	logout: 'Log out',
	home: 'Home',
	settings: 'Settings',
	welcome: 'Welcome, {name}',
	home_empty: 'Your contacts and money records will appear here.',
	settings_title: 'Ledger settings',
	currency: 'Currency',
	timezone: 'Timezone',
	send_hour: 'Reminder time',
	send_hour_hint: 'Reminders are sent at this time in your timezone.',
	email_language: 'Email language',
	email_language_hint: 'Language of the reminder emails your contacts receive.',
	save: 'Save',
	saving: 'Saving…',
	saved: 'Saved',
	owner_only: 'Only the owner can change these settings.',
	show_password: 'Show password',
	hide_password: 'Hide password',
	continue_with_google: 'Continue with Google',
	setup_with_google: 'Set up with Google',
	or: 'or',
	or_with_password: 'or set a password',
	err_google_failed: 'Google sign-in did not work. Please try again.',
	err_google_cancelled: 'Google sign-in was cancelled.',
	err_google_unverified: 'Your Google email is not verified.',
	err_no_account: 'No account uses this Google email. Ask the owner to add you, or log in with your password.',
	err_required: 'Required',
	err_too_short: 'Too short',
	err_too_long: 'Too long',
	err_invalid: 'Not valid',
	err_invalid_credentials: 'Email or password is wrong.',
	err_rate_limited: 'Too many attempts. Try again in 15 minutes.',
	err_already_set_up: 'This server is already set up.',
	err_forbidden: "You don't have permission to do this.",
	err_network: 'Cannot reach the server. Check your connection.',
	err_generic: 'Something went wrong. Please try again.'
};

export type Key = keyof typeof en;

const bn: Record<Key, string> = {
	app_name: 'টাকা পয়সা',
	setup_title: 'টাকা পয়সা চালু করুন',
	setup_intro: 'এই সার্ভারের মালিকের অ্যাকাউন্ট তৈরি করুন।',
	your_name: 'আপনার নাম',
	email: 'ইমেইল',
	password: 'পাসওয়ার্ড',
	password_hint: 'কমপক্ষে ৮ অক্ষর',
	book_name: 'খাতার নাম',
	book_name_hint: 'যেমন: আমার খাতা, রহিম স্টোর',
	language: 'ভাষা',
	create_account: 'অ্যাকাউন্ট তৈরি করুন',
	login_title: 'লগ ইন',
	login: 'লগ ইন করুন',
	logout: 'লগ আউট',
	home: 'হোম',
	settings: 'সেটিংস',
	welcome: 'স্বাগতম, {name}',
	home_empty: 'আপনার পরিচিতি আর টাকার হিসাব এখানে দেখাবে।',
	settings_title: 'খাতার সেটিংস',
	currency: 'মুদ্রা',
	timezone: 'সময় অঞ্চল',
	send_hour: 'রিমাইন্ডারের সময়',
	send_hour_hint: 'আপনার সময় অঞ্চলে এই সময়ে রিমাইন্ডার পাঠানো হবে।',
	email_language: 'ইমেইলের ভাষা',
	email_language_hint: 'আপনার পরিচিতিরা যে ভাষায় রিমাইন্ডার ইমেইল পাবেন।',
	save: 'সংরক্ষণ করুন',
	saving: 'সংরক্ষণ হচ্ছে…',
	saved: 'সংরক্ষিত হয়েছে',
	owner_only: 'শুধু মালিক এই সেটিংস বদলাতে পারেন।',
	show_password: 'পাসওয়ার্ড দেখান',
	hide_password: 'পাসওয়ার্ড লুকান',
	continue_with_google: 'Google দিয়ে চালিয়ে যান',
	setup_with_google: 'Google দিয়ে চালু করুন',
	or: 'অথবা',
	or_with_password: 'অথবা পাসওয়ার্ড দিন',
	err_google_failed: 'Google দিয়ে লগ ইন হয়নি। আবার চেষ্টা করুন।',
	err_google_cancelled: 'Google লগ ইন বাতিল করা হয়েছে।',
	err_google_unverified: 'আপনার Google ইমেইল যাচাই করা নেই।',
	err_no_account: 'এই Google ইমেইলের কোনো অ্যাকাউন্ট নেই। মালিককে আপনাকে যুক্ত করতে বলুন, অথবা পাসওয়ার্ড দিয়ে লগ ইন করুন।',
	err_required: 'আবশ্যক',
	err_too_short: 'খুব ছোট',
	err_too_long: 'খুব বড়',
	err_invalid: 'সঠিক নয়',
	err_invalid_credentials: 'ইমেইল বা পাসওয়ার্ড ভুল।',
	err_rate_limited: 'অনেকবার চেষ্টা হয়েছে। ১৫ মিনিট পরে আবার চেষ্টা করুন।',
	err_already_set_up: 'এই সার্ভার আগেই চালু করা হয়েছে।',
	err_forbidden: 'এটি করার অনুমতি আপনার নেই।',
	err_network: 'সার্ভারে পৌঁছানো যাচ্ছে না। ইন্টারনেট সংযোগ দেখুন।',
	err_generic: 'কিছু একটা সমস্যা হয়েছে। আবার চেষ্টা করুন।'
};

export type Locale = 'bn' | 'en';
const dicts: Record<Locale, Record<Key, string>> = { en, bn };

function initialLocale(): Locale {
	try {
		const saved = localStorage.getItem('locale');
		if (saved === 'bn' || saved === 'en') return saved;
	} catch {
		// storage unavailable (private mode): fall through to the default
	}
	return 'bn';
}

let current = $state<Locale>(initialLocale());

export const i18n = {
	get locale() {
		return current;
	},
	set locale(v: Locale) {
		current = v;
		document.documentElement.lang = v;
		try {
			localStorage.setItem('locale', v);
		} catch {
			// not persisted; fine
		}
	}
};

/** Translates a key, filling {placeholders} from vars. */
export function t(key: Key, vars: Record<string, string> = {}): string {
	return dicts[current][key].replace(/\{(\w+)\}/g, (_, k) => vars[k] ?? '');
}

/** Message for an API error code (top-level or per-field). */
export function errorText(code: string): string {
	const key = `err_${code}` as Key;
	return key in en ? t(key) : t('err_generic');
}
