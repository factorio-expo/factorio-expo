// The upload form's "From file" tab: a text file picked from the computer is
// read in the browser (it is never sent anywhere on its own) and its content
// goes into a read-only box that is inspected and uploaded exactly like a
// pasted string.
import type { UploadSource } from "./upload-source";
import { attachBlueprintInspect, type InspectionView } from "./blueprint-inspect";

/** Larger files are not blueprint strings (the largest fixtures are a few hundred KB). */
const MAX_FILE_BYTES = 8 * 1024 * 1024;

export function attachFileSource(panel: HTMLElement, view: InspectionView): UploadSource {
	const input = panel.querySelector<HTMLInputElement>("[data-file-input]")!;
	const text = panel.querySelector<HTMLTextAreaElement>("[data-file-text]")!;
	const status = panel.querySelector<HTMLElement>("[data-file-status]")!;
	const source = attachBlueprintInspect(text, view);

	input.addEventListener("change", async () => {
		const file = input.files?.[0];
		status.textContent = "";
		if (!file) return;
		if (file.size > MAX_FILE_BYTES) {
			text.value = "";
			status.textContent = "That file is too large to be a blueprint string.";
		} else {
			try {
				text.value = (await file.text()).trim();
				status.textContent = text.value ? file.name : `${file.name} is empty.`;
			} catch {
				text.value = "";
				status.textContent = "Could not read that file.";
			}
		}
		// As if pasted: the inspection and the upload button follow.
		text.dispatchEvent(new Event("change"));
	});
	return source;
}
