let message = $state('');
let timer: ReturnType<typeof setTimeout> | undefined;

/** One app-wide M3 snackbar: a short confirmation that hides itself after 4 seconds. */
export const snackbar = {
	get message() {
		return message;
	},
	show(text: string) {
		clearTimeout(timer);
		message = text;
		timer = setTimeout(() => (message = ''), 4000);
	}
};
