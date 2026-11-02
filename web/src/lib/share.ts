/** Opens the phone's share sheet, or copies to the clipboard where sharing isn't available. Returns true if copied. */
export async function shareOrCopy(url: string, title: string): Promise<boolean> {
	if (navigator.share) {
		await navigator.share({ title, url }).catch(() => {});
		return false;
	}
	await navigator.clipboard.writeText(url);
	return true;
}
