// The challenge page's writes: enter an entry (challenge-picker.ts), withdraw
// it, give or take back a vote. Each reloads the page, which the server renders
// afresh.
import { browserApi, runAction } from "@lib/browser-action";
import { setupPicker } from "./challenge-picker";

const slug = document.querySelector<HTMLElement>("[data-challenge-slug]")?.dataset.challengeSlug;

if (slug) {
	const status = document.querySelector<HTMLElement>("[data-challenge-status]") ?? undefined;

	setupPicker(slug, status);

	const withdraw = document.querySelector<HTMLButtonElement>("[data-challenge-withdraw]");
	withdraw?.addEventListener("click", async () => {
		if (!confirm("Withdraw your entry from this challenge?")) return;
		const done = await runAction(
			{
				status,
				controls: [withdraw],
				busy: "Withdrawing...",
				fallback: "Could not withdraw your entry.",
				onUnauthorized: "login",
			},
			() =>
				browserApi()
					.challenges.withdraw(slug)
					.then(() => true),
		);
		if (done) location.reload();
	});

	const voteStatus = document.querySelector<HTMLElement>("[data-vote-status]") ?? undefined;
	for (const button of document.querySelectorAll<HTMLButtonElement>("[data-challenge-vote]")) {
		button.addEventListener("click", async () => {
			const entryId = button.dataset.challengeVote!;
			const voted = button.getAttribute("aria-pressed") === "true";
			const done = await runAction(
				{
					status: voteStatus,
					controls: [button],
					fallback: voted ? "Could not take back your vote." : "Could not vote.",
					onUnauthorized: "login",
				},
				() => {
					const challenges = browserApi().challenges;
					return (
						voted ? challenges.unvote(slug, entryId) : challenges.vote(slug, entryId)
					).then(() => true);
				},
			);
			if (done) location.reload();
		});
	}
}
