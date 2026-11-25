// The bug button's dialog (ProblemReport.astro): gathers the page's context
// when it opens, shows it, and sends the report. Reopened after a sent
// report, it starts over.
import type { ProblemReportInput } from "@lib/api";
import { browserApi, runCheckedAction, turnstileField } from "@lib/browser-action";
import { problemContextOf, problemContextRows } from "@lib/problem-report";
import { setStatus } from "@lib/status";
import { mountHumanCheck, noHumanCheck, type HumanCheck } from "@lib/turnstile";

const root = document.querySelector<HTMLElement>("[data-problem-report]");
if (root) {
	const dialog = root.querySelector<HTMLDialogElement>("dialog")!;
	const form = dialog.querySelector<HTMLFormElement>("[data-problem-report-form]")!;
	const list = form.querySelector<HTMLElement>("[data-problem-report-context]")!;
	const status = form.querySelector<HTMLElement>("[data-problem-report-status]")!;
	const done = dialog.querySelector<HTMLElement>("[data-problem-report-done]")!;
	const reply = dialog.querySelector<HTMLElement>("[data-problem-report-reply]")!;
	let humanCheck: Promise<HumanCheck> | undefined;
	let opener: HTMLElement | undefined;
	let report: Omit<ProblemReportInput, "body" | "email"> = { page: location.pathname };

	const open = (from: HTMLElement) => {
		opener = from;
		if (!done.hidden) {
			form.reset();
			form.hidden = false;
			done.hidden = true;
			void humanCheck?.then((check) => check.reset());
		}
		setStatus(status, "");
		report = problemContextOf(
			location.pathname,
			document.querySelector<HTMLElement>("[data-problem-context]")?.dataset.problemContext,
			{
				viewport: `${window.innerWidth}x${window.innerHeight}`,
				theme: document.documentElement.dataset.theme ?? "",
			},
		);
		list.replaceChildren(
			...problemContextRows(report).flatMap(([label, value]) => {
				const term = document.createElement("dt");
				term.textContent = label;
				const detail = document.createElement("dd");
				detail.textContent = value;
				return [term, detail];
			}),
		);
		dialog.showModal();
		humanCheck ??= mountHumanCheck(form.querySelector<HTMLElement>("[data-turnstile]"));
	};

	document.addEventListener("click", (event) => {
		const target = event.target;
		if (!(target instanceof Element)) return;
		const trigger = target.closest<HTMLElement>("[data-problem-report-open]");
		if (!trigger) return;
		event.preventDefault();
		open(trigger);
	});
	dialog.addEventListener("close", () => opener?.focus());
	dialog
		.querySelector<HTMLElement>("[data-dialog-close]")
		?.addEventListener("click", () => dialog.close());
	dialog.addEventListener("click", (event) => {
		if (event.target === dialog) dialog.close();
	});

	form.addEventListener("submit", async (event) => {
		event.preventDefault();
		const body = form.querySelector<HTMLTextAreaElement>('[name="body"]')!.value.trim();
		const email = form.querySelector<HTMLInputElement>('[name="email"]')!.value.trim();
		if (!body) return;
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
					.site.reportProblem({
						...report,
						body,
						...(email ? { email } : {}),
						...turnstileField(token),
					})
					.then(() => true),
		);
		if (!sent) return;
		form.hidden = true;
		done.hidden = false;
		reply.hidden = !email;
		reply.textContent = email
			? `If an admin needs to know more, they will write to ${email}.`
			: "";
	});
}
