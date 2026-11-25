// Upload / new-revision submission for SubmissionForm.
import { browserApi, runCheckedAction, turnstileField } from "@lib/browser-action";
import { mountHumanCheck } from "@lib/turnstile";
import { bindFileDrop } from "@lib/file-drop";
import { setStatus } from "@lib/status";
import { editorLinks, entryPath } from "@lib/entry-links";
import { rememberEditLink } from "@lib/edit-link";
import { submittedEntryDetails } from "@components/entry-details/entry-details-form";
import type { BlueprintInspection } from "@lib/api";
import { attachBlueprintInspect, inspectionView, tagSuggester } from "./blueprint-inspect";
import { attachImport } from "./submission-import";
import { attachFileSource } from "./upload-file";
import { attachUploadTabs } from "./upload-tabs";
import type { UploadSource } from "./upload-source";

/** Resets every field and lets each tab's inspection drop its summary. */
function clearUploadForm(form: HTMLFormElement) {
	form.reset();
	for (const field of form.querySelectorAll("textarea")) field.dispatchEvent(new Event("change"));
	for (const status of form.querySelectorAll("[data-file-status], [data-import-status]"))
		status.textContent = "";
}

for (const form of document.querySelectorAll<HTMLFormElement>("[data-submission-form]")) {
	const entryId = form.dataset.entryId;
	const fields = form.querySelector<HTMLFieldSetElement>("[data-submission-fields]")!;
	const status = form.querySelector<HTMLParagraphElement>("[data-submission-status]")!;
	const blueprint = form.querySelector<HTMLTextAreaElement>('[name="blueprint"]')!;
	const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
	const note = form.querySelector<HTMLTextAreaElement>('[name="note"]');
	const suggestTags = tagSuggester(form);

	// A text file dropped on the box is read as if pasted.
	bindFileDrop(blueprint, (message) => setStatus(status, message));

	// The paste tab (a revision's only source): the string typed or pasted.
	const paste = attachBlueprintInspect(
		blueprint,
		inspectionView(
			form,
			form.querySelector<HTMLElement>("[data-blueprint-summary]")!,
			form.querySelector<HTMLButtonElement>("[data-copy-blueprint-details]"),
		),
	);

	// Uploads: the import tab and the tabs between the two. Its human check
	// starts when the tab is first shown (the widget needs a visible box).
	const tabsRoot = form.querySelector<HTMLElement>("[data-upload-tabs]");
	const importPanel = form.querySelector<HTMLElement>("[data-import]");
	let importCheck: ReturnType<typeof mountHumanCheck> | undefined;
	const importHumanCheck = () =>
		(importCheck ??= mountHumanCheck(
			form.querySelector<HTMLElement>("[data-import-human] [data-turnstile]"),
		));
	const link =
		importPanel &&
		attachImport(
			form,
			importPanel,
			inspectionView(
				form,
				importPanel.querySelector<HTMLElement>("[data-import-summary]")!,
				importPanel.querySelector<HTMLButtonElement>("[data-copy-import-details]"),
			),
			importHumanCheck,
		);

	// The file tab: a text file read here, then treated as pasted.
	const filePanel = form.querySelector<HTMLElement>("[data-file]");
	const file =
		filePanel &&
		attachFileSource(
			filePanel,
			inspectionView(
				form,
				filePanel.querySelector<HTMLElement>("[data-file-summary]")!,
				filePanel.querySelector<HTMLButtonElement>("[data-copy-file-details]"),
			),
		);

	// What the suggested tags last showed, so a refresh that changes nothing
	// leaves the pills the uploader may have used alone.
	let shownSource: UploadSource | undefined;
	let shownInspection: BlueprintInspection | undefined;
	const tabs =
		tabsRoot && link && file
			? attachUploadTabs(
					tabsRoot,
					[
						{ name: "paste", source: paste },
						{ name: "file", source: file },
						{ name: "import", source: link, onShow: () => void importHumanCheck() },
					],
					() => refresh(),
				)
			: undefined;
	const activeSource = () => tabs?.active() ?? paste;
	// An upload goes only with a valid, inspected blueprint in the active tab; a
	// revision keeps the API as its judge.
	function refresh() {
		const source = activeSource();
		const inspection = source.inspection();
		if (source !== shownSource || inspection !== shownInspection) {
			shownSource = source;
			shownInspection = inspection;
			suggestTags(inspection?.suggested_tags ?? []);
		}
		if (!entryId) submit.disabled = !source.submittable();
	}
	paste.onChange(refresh);
	file?.onChange(refresh);
	link?.onChange(refresh);

	// Signed-out visitors pass Turnstile (rendered only for them).
	const humanCheck = mountHumanCheck(
		form.querySelector<HTMLElement>("[data-submit-check] [data-turnstile]"),
	);

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const source = activeSource();
		if (entryId && !blueprint.value.trim()) {
			blueprint.setCustomValidity("Paste a blueprint exchange string.");
			blueprint.reportValidity();
			blueprint.setCustomValidity("");
			return;
		}
		if (!entryId && !source.submittable()) return;
		if (!form.reportValidity()) return;
		// A new revision leaves the entry details alone.
		const details = entryId ? undefined : submittedEntryDetails(form);
		if (!entryId && !details) return;
		const client = browserApi();
		const entry = await runCheckedAction(
			await humanCheck,
			{
				status,
				controls: [fields],
				busy: entryId ? "Creating revision..." : "Uploading...",
				fallback: "Submission failed.",
				onUnauthorized: "clear",
			},
			(token) =>
				entryId
					? client.entries.createRevision(entryId, {
							blueprint: blueprint.value,
							// An empty note is no note.
							...(note?.value.trim() ? { note: note.value.trim() } : {}),
							...turnstileField(token),
						})
					: client.entries.create({
							blueprint: source.exchange(),
							...details,
							// An import names its source and proves it (the fetch's token).
							...source.upload(),
							...turnstileField(token),
						}),
		);
		if (!entry) return;
		// An upload leaves the form empty, so the browser's back button (or its
		// restored form state) never shows the submitted fields again.
		if (!entryId) clearUploadForm(form);
		if (entry.edit_token) {
			// A new anonymous entry opens on its clean link, which is safe to share;
			// the entry page shows the edit link once (EditLinkBanner).
			rememberEditLink(entry.id, entry.edit_token);
			location.assign(entryPath(entry.id));
		} else {
			// A new revision made through an edit link stays on the edit link.
			location.assign(editorLinks(location.href).entry(entry.id));
		}
	});
	// Rendered disabled until this handler exists (see SubmissionForm): an upload
	// then waits for its blueprint's check.
	refresh();
	if (entryId) submit.disabled = false;
}
