// Inspects the pasted exchange string for SubmissionForm: a summary line and,
// on uploads, a button that copies the root's label and description into the
// entry details, and the suggested tags listed under the tag picker. Runs a moment after a paste or when the field loses focus,
// never per keystroke. A failure is not an error for the form: the upload
// still works, only these helpers stay empty. The imported blueprint of the
// import tab is shown by the same view (inspectionView), from the inspection
// the resolve call returned.
import { ApiError, type BlueprintInspection } from "@lib/api";
import { browserApi } from "@lib/browser-action";
import { inspectionDetails, inspectionSummary } from "@lib/blueprint-inspection";
import type { UploadSource } from "./upload-source";

const INPUT_DELAY_MS = 800;

/** Shows an inspection: a summary line and the button that copies its label and description. */
export interface InspectionView {
	/** Text for the summary line ("" hides it); `inspection` enables the copy button. */
	show(text: string, inspection?: BlueprintInspection): void;
}

/** The summary line and copy button of one tab; either may be missing (the revision form has no copy button). */
export function inspectionView(
	form: HTMLFormElement,
	summary: HTMLElement,
	copy: HTMLButtonElement | null,
): InspectionView {
	let current: BlueprintInspection | undefined;
	copy?.addEventListener("click", () => {
		if (!current) return;
		const details = inspectionDetails(current);
		fillField(form, "title", details.title);
		fillField(form, "description", details.description);
	});
	return {
		show(text, inspection) {
			summary.textContent = text;
			summary.hidden = !text;
			current = inspection;
			if (copy) {
				const details = inspection ? inspectionDetails(inspection) : {};
				copy.hidden = !details.title && !details.description;
			}
		},
	};
}

/**
 * Inspects what is typed into `blueprint`, showing the result in `view`. The
 * returned source says what the field holds: valid once the API parsed it,
 * invalid when it refused it, unknown when the check itself failed (a rate
 * limit or outage: the upload then judges the string).
 */
export function attachBlueprintInspect(
	blueprint: HTMLTextAreaElement,
	view: InspectionView,
): UploadSource {
	let inspected = "";
	let settled: { value: string; state: "valid" | "invalid" | "unknown" } | undefined;
	let current: BlueprintInspection | undefined;
	let pending: AbortController | undefined;
	let timer: ReturnType<typeof setTimeout> | undefined;
	const listeners: (() => void)[] = [];
	const changed = () => listeners.forEach((listener) => listener());

	const settle = (
		state: "valid" | "invalid" | "unknown",
		text: string,
		inspection?: BlueprintInspection,
	) => {
		settled = { value: blueprint.value, state };
		current = inspection;
		view.show(text, inspection);
		changed();
	};

	const inspect = async () => {
		clearTimeout(timer);
		// Sent as typed, like the upload, so the summary judges the same string.
		const value = blueprint.value;
		if (value === inspected) return;
		inspected = value;
		pending?.abort();
		settled = undefined;
		current = undefined;
		if (!value.trim()) {
			view.show("");
			changed();
			return;
		}
		const controller = new AbortController();
		pending = controller;
		changed();
		try {
			const inspection = await browserApi().blueprints.inspect(value, {
				signal: controller.signal,
			});
			settle("valid", inspectionSummary(inspection), inspection);
		} catch (err) {
			if (controller.signal.aborted) return;
			// A rejected string is explained. Anything else (rate limit, outage)
			// leaves the helpers out, and a later change retries.
			if (err instanceof ApiError && err.status === 422) {
				settle("invalid", err.detail ?? "That is not a valid blueprint.");
			} else {
				settle("unknown", "");
				inspected = "";
			}
		}
	};

	blueprint.addEventListener("input", () => {
		clearTimeout(timer);
		// Until the check has run for the new text, the old verdict does not apply
		// (state() compares them).
		changed();
		timer = setTimeout(() => void inspect(), INPUT_DELAY_MS);
	});
	blueprint.addEventListener("change", () => void inspect());

	return {
		exchange: () => blueprint.value,
		state() {
			if (!blueprint.value.trim()) return "empty";
			if (!settled || settled.value !== blueprint.value) return "checking";
			return settled.state;
		},
		inspection: () => (settled?.value === blueprint.value ? current : undefined),
		submittable() {
			const state = this.state();
			return state === "valid" || state === "unknown";
		},
		upload: () => ({}),
		onChange: (listener) => void listeners.push(listener),
	};
}

/**
 * Returns a function that lists suggested tags as pills under the upload's tag
 * picker (TagSuggestions). A click on a pill ticks its tag; "Add all" ticks
 * them all. Tags already ticked are left out, and a pill goes away once its
 * tag is ticked, by the pill or in the picker.
 */
export function tagSuggester(form: HTMLFormElement): (suggested: string[]) => void {
	const row = form.querySelector<HTMLElement>("[data-tag-suggestions]");
	const list = row?.querySelector<HTMLUListElement>("[data-tag-suggestion-list]");
	const template = row?.querySelector<HTMLTemplateElement>("[data-tag-suggestion-template]");
	const addAll = row?.querySelector<HTMLAnchorElement>("[data-tag-suggestion-all]");
	if (!row || !list || !template || !addAll) return () => {};

	const box = (slug: string) =>
		form.querySelector<HTMLInputElement>(
			`[data-entry-details] input[name="tags"][value="${CSS.escape(slug)}"]`,
		);
	const tick = (slugs: string[]) => {
		let ticked: HTMLInputElement | undefined;
		for (const slug of slugs) {
			const input = box(slug);
			if (input && !input.checked) {
				input.checked = true;
				ticked = input;
			}
		}
		// The picker redraws its summary on a change event from any of its boxes,
		// which also prunes the pills (below).
		ticked?.dispatchEvent(new Event("change", { bubbles: true }));
	};
	const prune = () => {
		for (const pill of list.querySelectorAll<HTMLElement>("li[data-slug]")) {
			if (box(pill.dataset.slug!)?.checked !== false) pill.remove();
		}
		row.hidden = list.childElementCount === 0;
	};

	form.addEventListener("change", (event) => {
		if (event.target instanceof HTMLInputElement && event.target.name === "tags") prune();
	});
	addAll.addEventListener("click", (event) => {
		event.preventDefault();
		tick(
			Array.from(
				list.querySelectorAll<HTMLElement>("li[data-slug]"),
				(pill) => pill.dataset.slug!,
			),
		);
	});

	return (suggested) => {
		list.replaceChildren();
		for (const slug of suggested) {
			const input = box(slug);
			if (!input || input.checked) continue;
			const pill = template.content.firstElementChild?.cloneNode(true);
			if (!(pill instanceof HTMLLIElement)) continue;
			pill.dataset.slug = slug;
			const chip = pill.querySelector<HTMLElement>(".tag-chip")!;
			const name = input.dataset.optionLabel ?? slug;
			chip.replaceChildren();
			// The picker's own chip for this tag holds the tag's icon, if it has one.
			const icon = input.parentElement?.querySelector(".tag-chip .factorio-icon");
			if (icon) chip.append(icon.cloneNode(true));
			const label = document.createElement("span");
			label.className = "label";
			label.textContent = name;
			chip.append(label);
			chip.title = `Add the tag ${name}`;
			pill.querySelector("button")!.addEventListener("click", () => tick([slug]));
			list.append(pill);
		}
		row.hidden = list.childElementCount === 0;
	};
}

/** Sets one entry details field; undefined leaves it as it is. */
export function fillField(form: HTMLFormElement, name: string, value: string | undefined) {
	const field = form.querySelector<HTMLInputElement | HTMLTextAreaElement>(
		`[data-entry-details] [name="${name}"]`,
	);
	if (!field || value === undefined) return;
	field.value = value;
	// The Markdown editor and icon picker follow their field's input events.
	field.dispatchEvent(new Event("input", { bubbles: true }));
}
