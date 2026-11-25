// The upload form's "Import from link" tab (SubmissionForm): paste a link from
// a supported site, the API fetches the blueprint (the browser never does) and
// inspects it, and the tab shows the same summary the pasted string gets. The
// imported string stays in this tab (the paste tab keeps what was typed
// there); the upload sends it with the source link and the token the import
// returned, which proves the string came from that link. The uploader must
// state they may share it, before the import and again at submit. Signed-out
// visitors pass a human check before the import. The site's own title and
// description (and the blueprint's label) are offered by buttons, never filled
// in on their own.
import { inspectionSummary } from "@lib/blueprint-inspection";
import { browserApi, runCheckedAction } from "@lib/browser-action";
import { setStatus } from "@lib/status";
import type { HumanCheck } from "@lib/turnstile";
import { fillField, type InspectionView } from "./blueprint-inspect";
import type { UploadSource } from "./upload-source";
import type { BlueprintInspection } from "@lib/api";

export const CONSENT_TEXT = "I made this blueprint or have the right to share it here";

const IMPORTED = "Imported. Check the details below, then upload.";
const TITLE_MAX = 160;
const DESCRIPTION_MAX = 5000;

/** Wires the import panel of an upload form. `humanCheck` starts the panel's check (once). */
export function attachImport(
	form: HTMLFormElement,
	panel: HTMLElement,
	view: InspectionView,
	humanCheck: () => Promise<HumanCheck>,
): UploadSource {
	const link = panel.querySelector<HTMLInputElement>("[data-import-link]")!;
	const consent = panel.querySelector<HTMLInputElement>("[data-import-consent]")!;
	const importButton = panel.querySelector<HTMLButtonElement>("[data-import-fetch]")!;
	const status = panel.querySelector<HTMLElement>("[data-import-status]")!;
	const siteButton = panel.querySelector<HTMLButtonElement>("[data-copy-site-details]")!;
	const note = panel.querySelector<HTMLElement>("[data-import-source]")!;
	const noteHost = note.querySelector<HTMLElement>("[data-import-source-host]")!;
	const listeners: (() => void)[] = [];
	const changed = () => listeners.forEach((listener) => listener());

	// The status line under the button; an error is shown red.
	const say = (text: string, error = false) => {
		setStatus(status, text);
		status.toggleAttribute("data-error", error && Boolean(text));
	};

	// What the last import returned, and the link text it was for.
	let imported:
		| {
				link: string;
				exchange: string;
				sourceUrl: string;
				sourceToken: string;
				inspection: BlueprintInspection;
				title?: string;
				description?: string;
		  }
		| undefined;
	const clear = () => {
		if (!imported) return;
		imported = undefined;
		note.hidden = true;
		siteButton.hidden = true;
		view.show("");
		changed();
	};
	// Another link is another import.
	link.addEventListener("input", () => {
		if (imported && link.value.trim() !== imported.link) {
			clear();
			say("");
		}
	});
	consent.addEventListener("change", () => {
		if (imported) {
			if (consent.checked) say(IMPORTED);
			else say(`Tick the box to upload an imported blueprint: ${CONSENT_TEXT}.`, true);
		}
		changed();
	});

	siteButton.addEventListener("click", () => {
		if (!imported) return;
		fillField(form, "title", imported.title?.slice(0, TITLE_MAX));
		fillField(form, "description", imported.description?.slice(0, DESCRIPTION_MAX));
	});

	importButton.addEventListener("click", async () => {
		if (!link.value.trim()) {
			say("Paste a link first.", true);
			link.focus();
			return;
		}
		if (!consent.checked) {
			say(`Tick the box first: ${CONSENT_TEXT}.`, true);
			consent.focus();
			return;
		}
		// A failed or pending import leaves nothing imported.
		clear();
		say("");
		const wanted = link.value.trim();
		const result = await runCheckedAction(
			await humanCheck(),
			{
				status,
				controls: [importButton, link],
				restore: true,
				busy: "Fetching the blueprint...",
				fallback: "Could not import that link.",
				onUnauthorized: "ignore",
			},
			(token) => browserApi().entries.resolveImport(wanted, true, token),
		);
		if (!result) {
			// Whatever the action left in the line is why it failed.
			status.toggleAttribute("data-error", Boolean(status.textContent));
			return;
		}
		imported = {
			link: wanted,
			exchange: result.blueprint,
			sourceUrl: result.source_url,
			sourceToken: result.source_token,
			inspection: result.inspection,
			title: result.title,
			description: result.description,
		};
		siteButton.hidden = !result.title && !result.description;
		noteHost.textContent = new URL(result.source_url).hostname;
		note.hidden = false;
		view.show(inspectionSummary(result.inspection), result.inspection);
		say(IMPORTED);
		changed();
	});

	return {
		exchange: () => imported?.exchange ?? "",
		state: () => (imported ? "valid" : "empty"),
		inspection: () => imported?.inspection,
		// The consent is stated again at submit: unticked after the import, the
		// button stays off (the status line says why).
		submittable: () => Boolean(imported) && consent.checked,
		upload: () =>
			imported
				? {
						source_url: imported.sourceUrl,
						source_token: imported.sourceToken,
						import_consent: true as const,
					}
				: {},
		onChange: (listener) => void listeners.push(listener),
	};
}
