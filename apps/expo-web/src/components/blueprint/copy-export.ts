// Copy-to-clipboard behaviour for BlueprintExport (lib/copy-button.ts).
import { bindCopyButton } from "@lib/copy-button";
import { bindDragString } from "@lib/drag-string";

for (const section of document.querySelectorAll<HTMLElement>("[data-blueprint-export]")) {
	const button = section.querySelector<HTMLButtonElement>("[data-copy-export]");
	const textarea = section.querySelector<HTMLTextAreaElement>("textarea");
	const status = section.querySelector<HTMLElement>(".copy-status");
	if (!button || !textarea || !status) continue;
	bindCopyButton(button, textarea, status);
	const handle = section.querySelector<HTMLElement>("[data-drag-string]");
	if (handle) bindDragString(handle, textarea);
}
