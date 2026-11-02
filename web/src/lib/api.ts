export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		public fields: Record<string, string> = {}
	) {
		super(code);
	}
}

export type Me = {
	user: { id: number; name: string; email: string; locale: 'bn' | 'en' };
	account: { id: number; role: 'owner' | 'member' };
};

export type AccountSettings = {
	name: string;
	currency: string;
	timezone: string;
	send_hour: number;
	locale: 'bn' | 'en';
};

/** Calls the JSON API. Throws ApiError with the server's error code and field errors. */
export async function api<T = void>(
	method: string,
	path: string,
	body?: unknown,
	f: typeof fetch = fetch
): Promise<T> {
	let res: Response;
	try {
		res = await f(`/api${path}`, {
			method,
			headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
			body: body === undefined ? undefined : JSON.stringify(body)
		});
	} catch {
		throw new ApiError(0, 'network');
	}
	const data = res.status === 204 ? null : await res.json().catch(() => null);
	if (!res.ok) throw new ApiError(res.status, data?.error ?? 'internal', data?.fields ?? {});
	return data as T;
}

export type Contact = {
	id: number;
	name: string;
	email: string;
	phone: string;
	locale: 'bn' | 'en';
	notes: string;
	tags: string[];
	email_opted_out: boolean;
};

export type ContactSummary = Contact & {
	receivable: number;
	payable: number;
	overdue: number;
	open_loans: number;
};

export type Direction = 'lent' | 'borrowed';
export type LoanStatus = 'requested' | 'approved' | 'active' | 'settled' | 'declined' | 'expired' | 'cancelled';
export type InstallmentStatus = 'paid' | 'partial' | 'overdue' | 'due_today' | 'upcoming';

export type LoanSummary = {
	id: number;
	contact_id: number;
	contact_name: string;
	direction: Direction;
	status: LoanStatus;
	currency: string;
	principal: number;
	paid: number;
	remaining: number;
	overdue: number;
	started_on: string;
	purpose: string;
	next_due_on: string | null;
	next_amount: number;
	origin: 'manual' | 'request' | 'link';
	request_message: string;
	request_expires_at: string | null;
	decision_note: string;
	sent_at: string | null;
	sent_method: string;
	sent_ref: string;
};

export type Installment = {
	id: number;
	seq: number;
	due_on: string;
	amount: number;
	paid: number;
	remaining: number;
	status: InstallmentStatus;
};

export type Payment = {
	id: number;
	amount: number;
	paid_on: string;
	method: string;
	note: string;
	source: 'owner' | 'contact_claim';
	status: 'confirmed' | 'pending' | 'rejected';
};

export type Activity = {
	id: number;
	action: string;
	data: Record<string, unknown>;
	actor_name: string;
	created_at: string;
};

export type Payout = {
	method: 'bkash' | 'nagad' | 'rocket' | 'bank' | 'cash';
	account_name: string;
	account_number: string;
	bank_name: string;
	branch: string;
	routing_number: string;
};

export type LoanDetail = LoanSummary & {
	installments: Installment[];
	payments: Payment[];
	activity: Activity[];
	payout: Payout | null;
};

export type DueItem = {
	agreement_id: number;
	contact_id: number;
	contact_name: string;
	direction: Direction;
	seq: number;
	count: number;
	due_on: string;
	remaining: number;
};

export type Overview = {
	currency: string;
	today: string;
	receivable: number;
	payable: number;
	overdue_receivable: number;
	overdue_payable: number;
	open_loans: number;
	due: DueItem[];
};

export type ScheduleInput =
	| { mode: 'generate'; count: number; first_due: string; frequency?: 'monthly' | 'weekly' | 'every_n_days'; every_days?: number }
	| { mode: 'custom'; items: { due_on: string; amount: number }[] };

export type Recipient = 'contact' | 'self';

export type ReminderRule = {
	id?: number;
	offset_days: number;
	repeat_every_days: number | null;
	repeat_count: number | null;
	send_hour: number | null;
	recipient: Recipient;
	channel?: 'email' | 'sms';
	tone: 'polite' | 'firm';
	paused: boolean;
};

export type ReminderSend = {
	id: number;
	rule_id: number | null;
	recipient: Recipient;
	channel: 'email' | 'sms';
	tone: 'polite' | 'firm';
	status: 'planned' | 'sending' | 'sent' | 'skipped' | 'failed';
	skip_reason: string;
	scheduled_for: string;
	sent_at: string | null;
};

export type ReminderView = {
	rules: ReminderRule[];
	upcoming: ReminderSend[];
	recent: ReminderSend[];
	contact_email: string;
	contact_opted_out: boolean;
	contact_phone: string;
	contact_sms_opted_out: boolean;
	direction: Direction;
};

export type Statement = {
	account_name: string;
	owner_name: string;
	contact_name: string;
	locale: 'bn' | 'en';
	currency: string;
	opted_out: boolean;
	has_email: boolean;
	sms_opted_out: boolean;
	has_phone: boolean;
	loans: (LoanSummary & { installments: Installment[]; payments: Payment[] })[];
};

export type RequestView = {
	id: number;
	status: LoanStatus;
	borrower_name: string;
	account_name: string;
	currency: string;
	principal: number;
	purpose: string;
	message: string;
	expires_at: string;
	locale: 'bn' | 'en';
	installments: { due_on: string; amount: number }[];
	decision_note: string;
};

export type RequestLink = {
	id: number;
	token: string;
	url: string;
	label: string;
	contact_id: number | null;
	contact_name: string;
	max_amount: number | null;
	open_count: number;
};

export type LinkInfo = {
	owner_name: string;
	account_name: string;
	currency: string;
	locale: 'bn' | 'en';
	max_amount: number | null;
	name: string;
	email: string;
	phone: string;
};
