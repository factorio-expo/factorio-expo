// The trending period tabs: show the chosen panel, no request. Arrow keys,
// Home and End move between tabs (the ARIA tabs pattern).

function bind(root: HTMLElement): void {
	const tabs = [...root.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
	const select = (tab: HTMLButtonElement, focus: boolean) => {
		for (const other of tabs) {
			const on = other === tab;
			other.setAttribute("aria-selected", String(on));
			other.tabIndex = on ? 0 : -1;
			const panel = document.getElementById(other.getAttribute("aria-controls") ?? "");
			if (panel) panel.hidden = !on;
		}
		if (focus) tab.focus();
	};
	for (const tab of tabs) {
		tab.addEventListener("click", () => select(tab, false));
		tab.addEventListener("keydown", (event) => {
			const index = tabs.indexOf(tab);
			const next = {
				ArrowRight: tabs[(index + 1) % tabs.length],
				ArrowLeft: tabs[(index - 1 + tabs.length) % tabs.length],
				Home: tabs[0],
				End: tabs[tabs.length - 1],
			}[event.key];
			if (!next) return;
			event.preventDefault();
			select(next, true);
		});
	}
}

for (const root of document.querySelectorAll<HTMLElement>("[data-trending-tabs]")) bind(root);
