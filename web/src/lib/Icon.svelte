<script lang="ts" module>
	// Material Symbols (outlined, weight 400), imported individually so only these ship.
	import arrowDropDown from '@material-symbols/svg-400/outlined/arrow_drop_down.svg?raw';
	import check from '@material-symbols/svg-400/outlined/check.svg?raw';
	import error from '@material-symbols/svg-400/outlined/error.svg?raw';
	import home from '@material-symbols/svg-400/outlined/home.svg?raw';
	import homeFill from '@material-symbols/svg-400/outlined/home-fill.svg?raw';
	import logout from '@material-symbols/svg-400/outlined/logout.svg?raw';
	import settings from '@material-symbols/svg-400/outlined/settings.svg?raw';
	import settingsFill from '@material-symbols/svg-400/outlined/settings-fill.svg?raw';
	import visibility from '@material-symbols/svg-400/outlined/visibility.svg?raw';
	import visibilityOff from '@material-symbols/svg-400/outlined/visibility_off.svg?raw';

	const icons = {
		arrow_drop_down: arrowDropDown,
		check,
		error,
		home,
		'home-fill': homeFill,
		logout,
		settings,
		'settings-fill': settingsFill,
		visibility,
		visibility_off: visibilityOff
	};

	export type IconName = keyof typeof icons;
</script>

<script lang="ts">
	let { name, size = 24 }: { name: IconName; size?: number } = $props();
</script>

<!-- Static SVG markup from the icon package, never user input. -->
<span class="md-icon" style:--size="{size}px" aria-hidden="true">{@html icons[name]}</span>
