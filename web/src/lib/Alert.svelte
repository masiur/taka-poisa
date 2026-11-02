<script lang="ts">
	import Icon from './Icon.svelte';

	let { message }: { message: string } = $props();
</script>

<div class="md-alert" role="alert">
	<Icon name="error" size={20} />
	<span>{message}</span>
</div>
