<script lang="ts">
	import { ApiError, type ReminderRule } from './api.js';
	import Alert from './Alert.svelte';
	import Dialog from './Dialog.svelte';
	import Icon from './Icon.svelte';
	import SelectField from './SelectField.svelte';
	import TextField from './TextField.svelte';
	import { errorText, i18n, t } from './i18n.svelte.js';
	import { presets } from './rules.js';

	let {
		rules,
		contactName = '',
		defaultHour,
		onsave
	}: {
		rules: ReminderRule[];
		contactName?: string;
		defaultHour: number;
		onsave: (rules: ReminderRule[]) => Promise<void>;
	} = $props();

	// svelte-ignore state_referenced_locally
	let list = $state<ReminderRule[]>(structuredClone(rules));
	let dirty = $state(false);
	let saving = $state(false);
	let saveError = $state('');

	function hourLabel(h: number) {
		return new Date(2000, 0, 1, h).toLocaleTimeString(i18n.locale === 'bn' ? 'bn-BD' : 'en-GB', { hour: 'numeric', minute: '2-digit' });
	}

	function num(n: number) {
		return n.toLocaleString(i18n.locale === 'bn' ? 'bn-BD' : 'en');
	}

	function when(r: ReminderRule) {
		if (r.offset_days === 0) return t('on_due_day');
		const n = Math.abs(r.offset_days);
		if (r.offset_days < 0) return n === 1 ? t('day_before') : t('days_before', { n: num(n) });
		return n === 1 ? t('day_late') : t('days_late', { n: num(n) });
	}

	function details(r: ReminderRule) {
		const parts = [
			r.recipient === 'self' ? t('to_me') : contactName ? t('to_contact', { name: contactName }) : t('to_contact_generic'),
			r.tone === 'firm' ? t('tone_firm') : t('tone_polite'),
			hourLabel(r.send_hour ?? defaultHour)
		];
		if (r.repeat_every_days && r.repeat_count) {
			parts.push(t('repeat_every', { every: num(r.repeat_every_days), count: num(r.repeat_count) }));
		}
		return parts.join(' · ');
	}

	function change(next: ReminderRule[]) {
		list = next;
		dirty = true;
	}

	// Add / edit dialog
	let open = $state(false);
	let editing = $state<number | null>(null);
	let form = $state({ offset: '0', recipient: 'contact', tone: 'polite', hour: '', repeat: false, every: '3', count: '3' });
	let formError = $state('');

	function openEditor(index: number | null) {
		editing = index;
		const r = index === null ? null : list[index];
		form = {
			offset: String(r?.offset_days ?? 0),
			recipient: r?.recipient ?? 'contact',
			tone: r?.tone ?? 'polite',
			hour: r?.send_hour == null ? '' : String(r.send_hour),
			repeat: !!r?.repeat_every_days,
			every: String(r?.repeat_every_days ?? 3),
			count: String(r?.repeat_count ?? 3)
		};
		formError = '';
		open = true;
	}

	function applyEditor(e: SubmitEvent) {
		e.preventDefault();
		const offset = Number(form.offset);
		const every = Number(form.every);
		const count = Number(form.count);
		if (!Number.isInteger(offset) || offset < -60 || offset > 60 || (form.repeat && (!(every >= 1 && every <= 30) || !(count >= 1 && count <= 20)))) {
			formError = errorText('invalid');
			return;
		}
		const r: ReminderRule = {
			offset_days: offset,
			recipient: form.recipient as ReminderRule['recipient'],
			tone: form.tone as ReminderRule['tone'],
			send_hour: form.hour === '' ? null : Number(form.hour),
			repeat_every_days: form.repeat ? every : null,
			repeat_count: form.repeat ? count : null,
			paused: editing === null ? false : list[editing].paused
		};
		change(editing === null ? [...list, r] : list.map((x, i) => (i === editing ? r : x)));
		open = false;
	}

	async function save() {
		saving = true;
		saveError = '';
		try {
			await onsave(list);
			dirty = false;
		} catch (err) {
			saveError = errorText(err instanceof ApiError ? (Object.values(err.fields)[0] ?? err.code) : 'generic');
		} finally {
			saving = false;
		}
	}
</script>

<div class="rules">
	<div class="presets" role="group" aria-label={t('presets')}>
		<span class="md-label-large md-muted">{t('presets')}:</span>
		{#each ['gentle', 'firm', 'early'] as const as p (p)}
			<button type="button" class="md-chip-button md-state" onclick={() => change(structuredClone(presets[p]))}>{t(`preset_${p}`)}</button>
		{/each}
	</div>

	{#if list.length === 0}
		<p class="md-body-medium md-muted">{t('no_reminders')}</p>
	{:else}
		<ul class="md-list">
			{#each list as r, i (i)}
				<li class="md-list__item rule" class:rule--paused={r.paused}>
					<Icon name={r.paused ? 'notifications_off' : 'notifications'} />
					<button type="button" class="md-list__main rule__text" onclick={() => openEditor(i)} aria-label="{t('edit_reminder')}: {when(r)}">
						<span class="md-body-large">{when(r)}{r.paused ? ` · ${t('paused')}` : ''}</span>
						<span class="md-body-small md-muted">{details(r)}</span>
					</button>
					<button type="button" class="md-icon-button md-state" title={r.paused ? t('resume') : t('pause')} aria-label={r.paused ? t('resume') : t('pause')}
						onclick={() => change(list.map((x, j) => (j === i ? { ...x, paused: !x.paused } : x)))}>
						<Icon name={r.paused ? 'play_arrow' : 'pause'} />
					</button>
					<button type="button" class="md-icon-button md-state" title={t('remove')} aria-label={t('remove')}
						onclick={() => change(list.filter((_, j) => j !== i))}>
						<Icon name="delete" />
					</button>
				</li>
			{/each}
		</ul>
	{/if}

	{#if saveError}<Alert message={saveError} />{/if}
	<div class="md-form__actions">
		{#if list.length < 10}
			<button type="button" class="md-button md-button--outlined md-state" onclick={() => openEditor(null)}><Icon name="add" size={18} />{t('add_reminder')}</button>
		{/if}
		{#if dirty}
			<button type="button" class="md-button md-button--filled md-state" onclick={save} disabled={saving}>{saving ? t('saving') : t('save')}</button>
		{/if}
	</div>
</div>

<Dialog bind:open title={editing === null ? t('add_reminder') : t('edit_reminder')}>
	<form id="rule_form" class="md-form" onsubmit={applyEditor} novalidate>
		<TextField id="rule_offset" label={t('offset_label')} hint={t('offset_hint')} bind:value={form.offset} inputmode="numeric" />
		<SelectField id="rule_recipient" label={t('send_to')} bind:value={form.recipient}>
			<option value="contact">{contactName ? t('to_contact', { name: contactName }) : t('to_contact_generic')}</option>
			<option value="self">{t('to_me')}</option>
		</SelectField>
		<SelectField id="rule_tone" label={t('tone')} bind:value={form.tone}>
			<option value="polite">{t('tone_polite')}</option>
			<option value="firm">{t('tone_firm')}</option>
		</SelectField>
		<SelectField id="rule_hour" label={t('send_time')} bind:value={form.hour}>
			<option value="">{t('default_time', { time: hourLabel(defaultHour) })}</option>
			{#each Array.from({ length: 24 }, (_, h) => h) as h (h)}<option value={String(h)}>{hourLabel(h)}</option>{/each}
		</SelectField>
		<label class="check md-body-large"><input type="checkbox" bind:checked={form.repeat} />{t('repeat')}</label>
		{#if form.repeat}
			<div class="row">
				<TextField id="rule_every" label={t('repeat_every_label')} bind:value={form.every} inputmode="numeric" />
				<TextField id="rule_count" label={t('repeat_count_label')} bind:value={form.count} inputmode="numeric" />
			</div>
		{/if}
		{#if formError}<Alert message={formError} />{/if}
	</form>
	{#snippet actions()}
		<button type="button" class="md-button md-button--text md-state" onclick={() => (open = false)}>{t('cancel')}</button>
		<button type="submit" form="rule_form" class="md-button md-button--filled md-state">{t('save')}</button>
	{/snippet}
</Dialog>

<style>
	.rules {
		display: grid;
		gap: 12px;
	}

	.presets {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}

	.rule {
		gap: 8px;
		padding-right: 0;
	}

	.rule--paused {
		opacity: 0.6;
	}

	.rule__text {
		border: 0;
		background: none;
		padding: 0;
		text-align: left;
		color: inherit;
		font: inherit;
		cursor: pointer;
	}

	.row {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	.check {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 48px;
		color: var(--md-sys-color-on-surface);
	}

	.check input {
		width: 18px;
		height: 18px;
		accent-color: var(--md-sys-color-primary);
	}
</style>
