<script lang="ts">
	import { t, type Key } from './i18n.svelte.js';

	let { status }: { status: string } = $props();

	// Colour roles per state; text always says the state too (never colour alone).
	const tone: Record<string, string> = {
		overdue: 'error',
		due_today: 'warning',
		partial: 'info',
		upcoming: 'neutral',
		paid: 'success',
		active: 'info',
		settled: 'success',
		cancelled: 'neutral',
		requested: 'warning',
		declined: 'neutral',
		expired: 'neutral'
	};
</script>

<span class="md-chip md-chip--{tone[status] ?? 'neutral'}">{t(`status_${status}` as Key)}</span>
