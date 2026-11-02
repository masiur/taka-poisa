<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		children,
		actions
	}: { open?: boolean; title: string; children: Snippet; actions: Snippet } = $props();

	let dialog: HTMLDialogElement;

	// A native modal <dialog>: focus trap, Escape to close and inert background come from the browser.
	$effect(() => {
		if (open && !dialog.open) dialog.showModal();
		if (!open && dialog.open) dialog.close();
	});
</script>

<dialog bind:this={dialog} class="md-dialog" aria-labelledby="dialog-title" onclose={() => (open = false)}>
	<h2 id="dialog-title" class="md-headline-small">{title}</h2>
	<div class="md-dialog__body">{@render children()}</div>
	<div class="md-dialog__actions">{@render actions()}</div>
</dialog>
