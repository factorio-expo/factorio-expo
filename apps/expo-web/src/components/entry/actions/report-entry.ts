// Report dialog behaviour for ReportEntryButton: "stolen" makes the note
// required, and a sent report swaps the form for a thank-you note.
import type { EntryReportReason } from "@lib/api";
import { bindDialog } from "@lib/dialog";
import { browserApi, runCheckedAction, turnstileField } from "@lib/browser-action";
import { setStatus } from "@lib/status";
import { mountHumanCheck, noHumanCheck, type HumanCheck } from "@lib/turnstile";

const REASONS: ReadonlySet<string> = new Set([
	"stolen",
	"offensive",
	"spam",
	"misleading",
	"other",
]);

function isReason(value: string | undefined): value is EntryReportReason {
	return value !== undefined && REASONS.has(value);
}

for (const root of document.querySelectorAll<HTMLElement>("[data-report-entry]")) {
	const entryId = root.dataset.reportEntry!;
	const opener = root.querySelector<HTMLButtonElement>("[data-report-entry-open]")!;
	const dialog = root.querySelector<HTMLDialogElement>("dialog")!;
	const form = dialog.querySelector<HTMLFormElement>("[data-report-entry-form]")!;
	const note = form.querySelector<HTMLTextAreaElement>('[name="note"]')!;
	const noteHint = form.querySelector<HTMLElement>("[data-report-note-hint]")!;
	const stolenHint = form.querySelector<HTMLElement>("[data-report-stolen-hint]")!;
	const status = form.querySelector<HTMLElement>("[data-report-entry-status]")!;
	const done = dialog.querySelector<HTMLElement>("[data-report-entry-done]")!;
	// Signed-out visitors pass Turnstile (rendered only for them), started
	// when the dialog first opens so a page view loads no Cloudflare script.
	let humanCheck: Promise<HumanCheck> | undefined;
	bindDialog({
		dialog,
		opener,
		onOpen: () => {
			setStatus(status, "");
			humanCheck ??= mountHumanCheck(form.querySelector<HTMLElement>("[data-turnstile]"));
		},
	});

	form.addEventListener("change", () => {
		const stolen =
			form.querySelector<HTMLInputElement>('[name="reason"]:checked')?.value === "stolen";
		note.required = stolen;
		noteHint.textContent = stolen ? "(required)" : "(optional)";
		stolenHint.hidden = !stolen;
	});

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const reason = form.querySelector<HTMLInputElement>('[name="reason"]:checked')?.value;
		if (!isReason(reason)) return;
		const sent = await runCheckedAction(
			await (humanCheck ?? Promise.resolve(noHumanCheck)),
			{
				status,
				controls: [form.querySelector<HTMLButtonElement>('button[type="submit"]')!],
				restore: true,
				busy: "Sending...",
				fallback: "Could not send the report.",
				onUnauthorized: "ignore",
			},
			(token) =>
				browserApi()
					.entries.report(entryId, {
						reason,
						note: note.value,
						revision_id: root.dataset.revisionId,
						...turnstileField(token),
					})
					.then(() => true),
		);
		if (!sent) return;
		form.hidden = true;
		done.hidden = false;
		opener.disabled = true;
		opener.dataset.tooltip = "Reported";
		opener.setAttribute("aria-label", "Entry reported");
	});
}
