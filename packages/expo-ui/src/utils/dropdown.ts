// One outside-click mechanism for every dropdown (MultiSelect, icon picker).
// Registered dropdowns close when a click lands outside them, which also keeps
// a single dropdown open at a time: opening one is a click outside the others.

export interface DropdownHandle {
	contains(node: Node): boolean;
	close(): void;
}

/** The registered dropdowns a click on `target` is outside of. */
export function dropdownsOutside(all: Iterable<DropdownHandle>, target: Node): DropdownHandle[] {
	return [...all].filter((dropdown) => !dropdown.contains(target));
}

const registered = new Set<DropdownHandle>();
let listening = false;

function onClick(event: MouseEvent) {
	if (!(event.target instanceof Node)) return;
	for (const dropdown of dropdownsOutside(registered, event.target)) dropdown.close();
}

/** Registers `dropdown`; the returned function unregisters it. */
export function registerDropdown(dropdown: DropdownHandle): () => void {
	registered.add(dropdown);
	if (!listening) {
		// Capture phase: runs even when something inside stops propagation.
		document.addEventListener("click", onClick, true);
		listening = true;
	}
	return () => {
		registered.delete(dropdown);
	};
}
