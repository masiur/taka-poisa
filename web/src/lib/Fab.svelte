<script lang="ts">
	import Icon, { type IconName } from './Icon.svelte';

	let { href, label, icon = 'add' }: { href: string; label: string; icon?: IconName } = $props();
</script>

<!-- Extended FAB, like Gmail's Compose button. -->
<a {href} class="md-fab md-state">
	<Icon name={icon} />
	<span>{label}</span>
</a>
