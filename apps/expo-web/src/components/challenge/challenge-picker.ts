// The challenge page's entry picker: a search box over the viewer's public
// single blueprints, loaded a page at a time from the candidates endpoint.
// Picking one enters it (replacing the current entry) and reloads the page.
import type { EntrySummary } from "@lib/api";
import { assetUrl } from "@lib/api/public-asset";
import { browserApi, runAction } from "@lib/browser-action";
import { setStatus } from "@lib/status";
import { stripRichText } from "@factorio-expo/ui/game/rich-text.ts";

const PAGE_SIZE = 12;
const SEARCH_DELAY_MS = 250;

export function setupPicker(slug: string, status: HTMLElement | undefined): void {
	const picker = document.querySelector<HTMLElement>("[data-challenge-picker]");
	if (!picker) return;
	const search = picker.querySelector<HTMLInputElement>("[data-picker-search]")!;
	const list = picker.querySelector<HTMLUListElement>("[data-picker-list]")!;
	const empty = picker.querySelector<HTMLElement>("[data-picker-empty]")!;
	const more = picker.querySelector<HTMLButtonElement>("[data-picker-more]")!;
	const currentId = picker.dataset.current;

	let query = "";
	let shown = 0;
	let loaded = false;
	// Answers of an older search arrive late: only the latest one counts.
	let generation = 0;

	async function load(reset: boolean) {
		const mine = ++generation;
		if (reset) shown = 0;
		const page = await runAction(
			{
				status,
				controls: [more],
				restore: true,
				fallback: "Could not load your blueprints.",
				onUnauthorized: "login",
			},
			() =>
				browserApi().challenges.candidates(slug, {
					q: query,
					limit: PAGE_SIZE,
					offset: shown,
				}),
		);
		if (!page || mine !== generation) return;
		if (reset) list.replaceChildren();
		for (const entry of page.entries) list.append(row(entry));
		shown += page.entries.length;
		more.hidden = shown >= page.total;
		empty.textContent =
			page.total === 0
				? query
					? "No public blueprint of yours matches."
					: "You have no public single blueprints yet."
				: "";
	}

	function row(entry: EntrySummary): HTMLLIElement {
		const title = stripRichText(entry.title || entry.label || "Untitled blueprint");
		const item = document.createElement("li");
		const button = document.createElement("button");
		button.type = "button";
		button.className = "pick secondary outline";
		const thumb = entry.preview_url
			? document.createElement("img")
			: document.createElement("span");
		if (thumb instanceof HTMLImageElement) {
			thumb.src = assetUrl(entry.preview_url!);
			thumb.alt = "";
			thumb.loading = "lazy";
		} else {
			thumb.className = "thumb";
		}
		const name = document.createElement("span");
		name.className = "name";
		name.textContent = entry.id === currentId ? `${title} (your entry)` : title;
		button.append(thumb, name);
		button.disabled = entry.id === currentId;
		button.addEventListener("click", () => enter(entry.id, title));
		item.append(button);
		return item;
	}

	async function enter(entryId: string, title: string) {
		if (currentId && !confirm(`Replace your entry with "${title}"?`)) return;
		const buttons = [...list.querySelectorAll<HTMLButtonElement>("button"), more];
		const done = await runAction(
			{
				status,
				controls: buttons,
				busy: "Entering...",
				fallback: "Could not enter this entry.",
				onUnauthorized: "login",
			},
			() =>
				browserApi()
					.challenges.submit(slug, entryId)
					.then(() => true),
		);
		if (done) location.reload();
	}

	let timer: ReturnType<typeof setTimeout> | undefined;
	search.addEventListener("input", () => {
		clearTimeout(timer);
		timer = setTimeout(() => {
			query = search.value.trim();
			void load(true);
		}, SEARCH_DELAY_MS);
	});
	more.addEventListener("click", () => void load(false));

	const open = () => {
		picker.hidden = false;
		if (!loaded) {
			loaded = true;
			void load(true);
		}
		search.focus();
	};
	const toggle = document.querySelector<HTMLButtonElement>("[data-challenge-pick-toggle]");
	toggle?.addEventListener("click", () => {
		if (picker.hidden) open();
		else {
			picker.hidden = true;
			if (status) setStatus(status, "");
		}
	});
	// Not entered yet: the picker is the page's call to action, open from the start.
	if (!picker.hidden) {
		loaded = true;
		void load(true);
	}
}
