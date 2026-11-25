// Behaviour of ShareButton: open and close the menu (outside click through
// lib/dropdown.ts, Escape), offer the system share sheet where it exists and
// copy the link.
import { registerDropdown } from "@factorio-expo/ui/utils/dropdown.ts";
import { nativeShare } from "@lib/share";

const COPY_LABELS = { idle: "Copy link", success: "Link copied", failed: "Copy failed" };

for (const root of document.querySelectorAll<HTMLElement>("[data-share]")) {
	const toggle = root.querySelector<HTMLButtonElement>("[data-share-toggle]")!;
	const menu = root.querySelector<HTMLElement>("[data-share-menu]")!;
	const nativeItem = root.querySelector<HTMLElement>("[data-share-native]")!;
	const copyLabel = root.querySelector<HTMLElement>("[data-share-copy-label]")!;
	const data = { url: root.dataset.shareUrl ?? "", title: root.dataset.shareTitle ?? "" };

	const setOpen = (open: boolean) => {
		menu.hidden = !open;
		toggle.setAttribute("aria-expanded", String(open));
	};
	registerDropdown({
		contains: (node) => root.contains(node),
		close: () => setOpen(false),
	});

	if (typeof navigator.share === "function") nativeItem.hidden = false;

	toggle.addEventListener("click", () => setOpen(Boolean(menu.hidden)));
	root.addEventListener("keydown", (event) => {
		if (event.key !== "Escape" || menu.hidden) return;
		setOpen(false);
		toggle.focus();
	});
	// The Reddit link opens in a new tab: nothing more to do than close.
	menu.querySelector("a")?.addEventListener("click", () => setOpen(false));

	root.querySelector("[data-share-native-button]")?.addEventListener("click", async () => {
		setOpen(false);
		await nativeShare(data);
	});

	let resetTimer: number | undefined;
	root.querySelector("[data-share-copy]")?.addEventListener("click", async () => {
		let state: keyof typeof COPY_LABELS = "success";
		try {
			await navigator.clipboard.writeText(data.url);
		} catch {
			state = "failed";
		}
		copyLabel.textContent = COPY_LABELS[state];
		window.clearTimeout(resetTimer);
		resetTimer = window.setTimeout(() => {
			copyLabel.textContent = COPY_LABELS.idle;
			setOpen(false);
		}, 1500);
	});
}
