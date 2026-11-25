// The upload form's source tabs (SubmissionForm): "Paste string", "From file" and
// "Import from link". The ARIA tabs pattern: arrow keys, Home and End move between
// tabs, only the selected tab is in the tab order. Switching only shows the
// other panel: nothing typed in either is touched.
import type { UploadSource } from "./upload-source";

/** One tab: its button, its panel and the source it stands for. */
export interface SourceTab {
	name: string;
	source: UploadSource;
	/** Called each time the tab is shown, for work that needs a visible panel (the link tab's human check). */
	onShow?: () => void;
}

export interface UploadTabs {
	/** The source of the selected tab. */
	active(): UploadSource;
}

/**
 * Wires `root` ([data-upload-tabs]): its `[role=tab]` buttons carry `data-tab`
 * names matching `tabs`. `changed` runs after every switch.
 */
export function attachUploadTabs(
	root: HTMLElement,
	tabs: SourceTab[],
	changed: () => void,
): UploadTabs {
	const buttons = [...root.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
	let current = tabs[0];

	const select = (button: HTMLButtonElement, focus: boolean) => {
		const tab = tabs.find((candidate) => candidate.name === button.dataset.tab);
		if (!tab) return;
		current = tab;
		for (const other of buttons) {
			const on = other === button;
			other.setAttribute("aria-selected", String(on));
			other.tabIndex = on ? 0 : -1;
			const panel = document.getElementById(other.getAttribute("aria-controls") ?? "");
			if (panel) panel.hidden = !on;
		}
		if (focus) button.focus();
		tab.onShow?.();
		changed();
	};

	for (const button of buttons) {
		button.addEventListener("click", () => select(button, false));
		button.addEventListener("keydown", (event) => {
			const index = buttons.indexOf(button);
			const next = {
				ArrowRight: buttons[(index + 1) % buttons.length],
				ArrowLeft: buttons[(index - 1 + buttons.length) % buttons.length],
				Home: buttons[0],
				End: buttons[buttons.length - 1],
			}[event.key];
			if (!next) return;
			event.preventDefault();
			select(next, true);
		});
	}
	return { active: () => current.source };
}
