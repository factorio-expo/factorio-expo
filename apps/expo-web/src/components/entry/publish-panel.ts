// Publish panel (PublishPanel.astro). On a quick preview: a countdown to the
// expiry, live progress until the preview and analysis settle, the failure
// that rejected the preview, and the publish form, enabled only once both are
// ready. On an anonymous entry: just the publish form.
import { entryEventsUrl } from "@lib/api/browser";
import { browserApi, runAction } from "@lib/browser-action";
import { previewSettlement, watchEntryProgress } from "@lib/entry-progress";
import { failureReason } from "@lib/failure-reason";
import { setStatus } from "@lib/status";
import { submittedEntryDetails } from "@components/entry-details/entry-details-form";
import { remainingText } from "@factorio-expo/ui/utils/dates.ts";

function bind(panel: HTMLElement) {
	const entryId = panel.dataset.entryId!;
	const expiry = panel.dataset.expiresAt;
	const countdown = panel.querySelector<HTMLElement>("[data-expiry-countdown]");
	const failure = panel.querySelector<HTMLElement>("[data-preview-failure]")!;
	const form = panel.querySelector<HTMLFormElement>("[data-publish-form]");
	const button = form?.querySelector<HTMLButtonElement>("[data-publish-button]");
	const status = form?.querySelector<HTMLElement>("[data-publish-status]");
	let ready = panel.dataset.ready !== undefined;
	let ended = false;

	const end = (message: string) => {
		ended = true;
		failure.textContent = message;
		failure.hidden = false;
		if (button) button.disabled = true;
		if (status) setStatus(status, "");
	};
	const sync = () => {
		if (button) button.disabled = ended || !ready;
		if (status && ready && !ended) setStatus(status, "");
	};
	const tick = (expiresAt: string, shown: HTMLElement) => {
		const left = remainingText(expiresAt);
		if (left === undefined) {
			shown.textContent = "";
			if (!ended) end("This preview has expired.");
			return;
		}
		shown.textContent = `(in ${left})`;
		window.setTimeout(() => tick(expiresAt, shown), 30_000);
	};
	if (expiry && countdown) tick(expiry, countdown);
	sync();

	if (!ready) {
		watchEntryProgress(
			{ eventsUrl: entryEventsUrl(entryId) },
			{
				onState: (state) => {
					const settlement = previewSettlement(state);
					if (settlement.failure) {
						const { code } = settlement.failure;
						const reason = failureReason(code) ?? "It could not be completed.";
						return end(
							`The preview failed: ${reason} Nothing was kept. If this looks wrong, use Report a problem at the bottom of the page.`,
						);
					}
					ready = settlement.ready;
					sync();
				},
				onGone: () => {
					if (!ended) end("This preview has expired or was removed.");
				},
			},
		);
	}

	form?.addEventListener("submit", async (event) => {
		event.preventDefault();
		if (!form.reportValidity() || !status) return;
		const details = submittedEntryDetails(form);
		if (!details) return;
		const published = await runAction(
			{
				status,
				controls: [form.querySelector<HTMLFieldSetElement>("[data-publish-fields]")!],
				busy: "Publishing...",
				fallback: "Could not publish the entry.",
				onUnauthorized: "login",
			},
			() =>
				browserApi().entries.publish(entryId, {
					...details,
					visibility: details.visibility ?? "public",
				}),
		);
		// Now an owned entry: its clean link carries everything.
		if (published) location.assign(`/e/${encodeURIComponent(published.id)}`);
	});
}

for (const panel of document.querySelectorAll<HTMLElement>("[data-publish-panel]")) bind(panel);
