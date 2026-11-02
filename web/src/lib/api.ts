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
