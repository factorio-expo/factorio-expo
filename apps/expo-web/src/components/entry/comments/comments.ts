// Comment thread behaviour for CommentThread: post, edit, remove and report.
// After a change the list (a live region) is re-rendered from the server, so
// Markdown is only ever rendered and sanitized there.
import { bindDialog } from "@lib/dialog";
import { browserApi, runAction } from "@lib/browser-action";
import { LIVE_REGIONS, onLiveRegions, refreshLiveRegions } from "@lib/live-regions";
import { setStatus } from "@lib/status";
import { upgradeTimes } from "@factorio-expo/ui/utils/dates.ts";
import type { CommentReportReason } from "@lib/api";

for (const thread of document.querySelectorAll<HTMLElement>("[data-comment-thread]")) {
	const entryId = thread.dataset.entryId!;
	bindNewComment(thread, entryId);
	const report = bindReportDialog(thread, entryId);
	onLiveRegions((root) => {
		// Other live regions (preview, analysis) refresh too; bind only ours.
		const list =
			root === document
				? thread.querySelector("[data-comment-list]")
				: root instanceof Element && root.matches("[data-comment-list]")
					? root
					: null;
		if (!list || !thread.contains(list)) return;
		upgradeTimes(list);
		for (const item of list.querySelectorAll<HTMLElement>("[data-comment]")) {
			bindComment(item, entryId, report);
		}
	});
}

/** Shows comment `number` once the list holds it. */
function reveal(number: number) {
	const target = document.getElementById(`c-${number}`);
	if (!target) return;
	history.replaceState(null, "", `#c-${number}`);
	target.scrollIntoView({ block: "center" });
}

function bindNewComment(thread: HTMLElement, entryId: string) {
	const form = thread.querySelector<HTMLFormElement>("[data-comment-form]");
	if (!form) return;
	const body = form.querySelector<HTMLTextAreaElement>('[name="body"]')!;
	const status = form.querySelector<HTMLElement>("[data-comment-form-status]")!;
	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
		const comment = await runAction(
			{
				status,
				controls: [submit],
				restore: true,
				busy: "Posting...",
				fallback: "Could not post the comment.",
				onUnauthorized: "login",
			},
			() => browserApi().comments.create(entryId, body.value, form.dataset.revisionId),
		);
		if (!comment) return;
		form.reset();
		// The markdown editor mirrors its textarea on input events.
		body.dispatchEvent(new Event("input", { bubbles: true }));
		const list = thread.querySelector<HTMLElement>("[data-comment-list]");
		if (list?.dataset.hasNewer === "true") {
			// This page is not the newest: open the one ending with the new comment.
			const url = new URL(location.href);
			url.searchParams.delete("comments_after");
			url.searchParams.set("comments_before", String(comment.number + 1));
			url.hash = `c-${comment.number}`;
			location.assign(url);
			return;
		}
		await refreshLiveRegions([LIVE_REGIONS.comments]);
		reveal(comment.number);
	});
}

function bindComment(item: HTMLElement, entryId: string, report: (number: number) => void) {
	const number = Number(item.dataset.comment);
	const status = item.querySelector<HTMLElement>("[data-comment-status]")!;

	const edit = item.querySelector<HTMLButtonElement>("[data-comment-edit]");
	const form = item.querySelector<HTMLFormElement>("[data-comment-edit-form]");
	const body = item.querySelector<HTMLElement>("[data-comment-body]");
	if (edit && form && body) {
		const textarea = form.querySelector<HTMLTextAreaElement>('[name="body"]')!;
		edit.addEventListener("click", () => {
			body.hidden = true;
			form.hidden = false;
			textarea.focus();
		});
		form.querySelector("[data-comment-edit-cancel]")!.addEventListener("click", () => {
			form.reset();
			form.hidden = true;
			body.hidden = false;
			setStatus(status, "");
			edit.focus();
		});
		form.addEventListener("submit", async (event) => {
			event.preventDefault();
			const saved = await runAction(
				{
					status,
					controls: [form.querySelector<HTMLButtonElement>('button[type="submit"]')!],
					busy: "Saving...",
					fallback: "Could not save the comment.",
					onUnauthorized: "login",
				},
				() => browserApi().comments.edit(entryId, number, textarea.value),
			);
			if (saved) {
				await refreshLiveRegions([LIVE_REGIONS.comments]);
				reveal(number);
			}
		});
	}

	const remove = item.querySelector<HTMLButtonElement>("[data-comment-remove]");
	remove?.addEventListener("click", async () => {
		if (!confirm(`Remove comment #${number}? Its text is deleted for good.`)) return;
		const removed = await runAction(
			{
				status,
				controls: [remove],
				busy: "Removing...",
				fallback: "Could not remove the comment.",
				// An edit-token holder moderates without a session.
				onUnauthorized: "clear",
			},
			() =>
				browserApi()
					.comments.remove(entryId, number)
					.then(() => true),
		);
		if (removed) await refreshLiveRegions([LIVE_REGIONS.comments]);
	});

	item.querySelector<HTMLButtonElement>("[data-comment-report]")?.addEventListener("click", () =>
		report(number),
	);
}

/** Binds the report dialog and returns how to open it for a comment. */
function bindReportDialog(thread: HTMLElement, entryId: string): (number: number) => void {
	const dialog = thread.querySelector<HTMLDialogElement>("dialog");
	const form = dialog?.querySelector<HTMLFormElement>("[data-comment-report-form]");
	if (!dialog || !form) return () => {};
	const status = form.querySelector<HTMLElement>("[data-comment-report-status]")!;
	const title = dialog.querySelector<HTMLElement>("h2")!;
	// bindDialog wants one opener; this hidden one stands in for the
	// per-comment buttons, so focus returns to the thread on close.
	const opener = document.createElement("button");
	opener.hidden = true;
	thread.append(opener);
	let number = 0;
	bindDialog({ dialog, opener });

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const reason = form.querySelector<HTMLInputElement>('[name="reason"]:checked')?.value;
		const note = form.querySelector<HTMLTextAreaElement>('[name="note"]')!.value;
		if (!isReportReason(reason)) return;
		const submit = form.querySelector<HTMLButtonElement>('button[type="submit"]')!;
		const sent = await runAction(
			{
				status,
				controls: [submit],
				restore: true,
				busy: "Sending...",
				fallback: "Could not send the report.",
				onUnauthorized: "login",
			},
			() =>
				browserApi()
					.comments.report(entryId, number, reason, note)
					.then(() => true),
		);
		if (!sent) return;
		form.reset();
		dialog.close();
		const item = document.getElementById(`c-${number}`);
		const itemStatus = item?.querySelector<HTMLElement>("[data-comment-status]");
		if (itemStatus) setStatus(itemStatus, "Thanks, a moderator will look at it.");
	});

	return (selected) => {
		number = selected;
		title.textContent = `Report comment #${selected}`;
		setStatus(status, "");
		opener.click();
	};
}

const REPORT_REASONS: ReadonlySet<string> = new Set(["spam", "abuse", "off_topic", "other"]);

function isReportReason(value: string | undefined): value is CommentReportReason {
	return value !== undefined && REPORT_REASONS.has(value);
}
