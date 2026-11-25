// The challenge upload form: checks the string is a single blueprint,
// uploads it as a public entry titled after its label, enters it in the
// challenge and opens the challenge page.
import { browserApi, runAction } from "@lib/browser-action";
import { entryPath } from "@lib/entry-links";
import { setStatus } from "@lib/status";

const MAX_TITLE = 160;

const form = document.querySelector<HTMLFormElement>("[data-challenge-upload]");
if (form) {
	const slug = form.dataset.slug!;
	const fields = form.querySelector<HTMLFieldSetElement>("[data-challenge-upload-fields]")!;
	const status = form.querySelector<HTMLElement>("[data-challenge-upload-status]")!;
	const blueprint = form.querySelector<HTMLTextAreaElement>('[name="blueprint"]')!;

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const client = browserApi();
		const exchange = blueprint.value.trim();
		const inspection = await runAction(
			{
				status,
				controls: [fields],
				busy: "Reading the blueprint...",
				// The form stays usable when the string is refused below.
				restore: true,
				fallback: "The blueprint could not be read.",
				onUnauthorized: "login",
			},
			() => client.blueprints.inspect(exchange),
		);
		if (!inspection) return;
		if (inspection.kind !== "blueprint") {
			setStatus(status, "Enter a single blueprint, not a book or a planner.");
			return;
		}
		const title = (inspection.label.trim() || form.dataset.fallbackTitle!).slice(0, MAX_TITLE);
		const entry = await runAction(
			{
				status,
				controls: [fields],
				busy: "Uploading...",
				fallback: "The upload failed.",
				onUnauthorized: "login",
			},
			() =>
				client.entries.create({
					blueprint: exchange,
					title,
					description: inspection.description.slice(0, 5000),
					visibility: "public",
				}),
		);
		if (!entry) return;
		const entered = await runAction(
			{
				status,
				controls: [fields],
				busy: "Entering the challenge...",
				fallback: "Uploaded, but it could not be entered in the challenge.",
				onUnauthorized: "login",
			},
			() => client.challenges.submit(slug, entry.id).then(() => true),
		);
		if (!entered) {
			const link = document.createElement("a");
			link.href = entryPath(entry.id);
			link.textContent = "Open the uploaded entry";
			status.append(" ", link);
			return;
		}
		location.assign(`/challenges/${encodeURIComponent(slug)}`);
	});
	form.querySelector<HTMLButtonElement>('button[type="submit"]')!.disabled = false;
}
