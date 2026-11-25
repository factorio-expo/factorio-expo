// Copy-to-clipboard icon buttons (BlueprintExport, StringTool). The button
// holds one icon per state (`data-copy-icon` idle / success / failed); feedback
// is its Pico tooltip (data-tooltip, held open by data-tip-open) plus `status`,
// a screen-reader-only live region. Import only from client-side code.
type CopyState = "idle" | "success" | "failed";

/** Copies `source`'s text on click; `idleTip` is the button's resting label. */
export function bindCopyButton(
	button: HTMLButtonElement,
	source: HTMLTextAreaElement,
	status: HTMLElement,
	idleTip = "Copy to clipboard",
) {
	let resetTimer: number | undefined;
	const setCopyState = (state: CopyState) => {
		for (const icon of button.querySelectorAll<HTMLElement>("[data-copy-icon]")) {
			icon.setAttribute("aria-hidden", String(icon.dataset.copyIcon !== state));
		}
		const tip =
			state === "success"
				? "Copied to clipboard"
				: state === "failed"
					? "Copy failed, select the text manually"
					: idleTip;
		button.dataset.tooltip = tip;
		button.setAttribute("aria-label", tip);
		status.textContent = state === "idle" ? "" : tip;
		button.toggleAttribute("data-tip-open", state !== "idle");
		window.clearTimeout(resetTimer);
		if (state !== "idle") resetTimer = window.setTimeout(() => setCopyState("idle"), 2500);
	};
	button.addEventListener("click", async () => {
		try {
			await navigator.clipboard.writeText(source.value);
			setCopyState("success");
		} catch {
			source.select();
			setCopyState("failed");
		}
	});
	setCopyState("idle");
}
