<script lang="ts" module>
	// Material Symbols (outlined, weight 400), imported individually so only these ship.
	import add from '@material-symbols/svg-400/outlined/add.svg?raw';
	import arrowBack from '@material-symbols/svg-400/outlined/arrow_back.svg?raw';
	import arrowDropDown from '@material-symbols/svg-400/outlined/arrow_drop_down.svg?raw';
	import call from '@material-symbols/svg-400/outlined/call.svg?raw';
	import check from '@material-symbols/svg-400/outlined/check.svg?raw';
	import close from '@material-symbols/svg-400/outlined/close.svg?raw';
	import del from '@material-symbols/svg-400/outlined/delete.svg?raw';
	import download from '@material-symbols/svg-400/outlined/download.svg?raw';
	import edit from '@material-symbols/svg-400/outlined/edit.svg?raw';
	import error from '@material-symbols/svg-400/outlined/error.svg?raw';
	import group from '@material-symbols/svg-400/outlined/group.svg?raw';
	import groupFill from '@material-symbols/svg-400/outlined/group-fill.svg?raw';
	import history from '@material-symbols/svg-400/outlined/history.svg?raw';
	import home from '@material-symbols/svg-400/outlined/home.svg?raw';
	import homeFill from '@material-symbols/svg-400/outlined/home-fill.svg?raw';
	import logout from '@material-symbols/svg-400/outlined/logout.svg?raw';
	import mail from '@material-symbols/svg-400/outlined/mail.svg?raw';
	import payments from '@material-symbols/svg-400/outlined/payments.svg?raw';
	import person from '@material-symbols/svg-400/outlined/person.svg?raw';
	import search from '@material-symbols/svg-400/outlined/search.svg?raw';
	import settings from '@material-symbols/svg-400/outlined/settings.svg?raw';
	import settingsFill from '@material-symbols/svg-400/outlined/settings-fill.svg?raw';
	import visibility from '@material-symbols/svg-400/outlined/visibility.svg?raw';
	import visibilityOff from '@material-symbols/svg-400/outlined/visibility_off.svg?raw';

	const icons = {
		add,
		arrow_back: arrowBack,
		arrow_drop_down: arrowDropDown,
		call,
		check,
		close,
		delete: del,
		download,
		edit,
		error,
		group,
		'group-fill': groupFill,
		history,
		home,
		'home-fill': homeFill,
		logout,
		mail,
		payments,
		person,
		search,
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
