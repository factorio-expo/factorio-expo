// Keeps an element's content fresh: <div data-poll="/partials/x"
// data-poll-seconds="5"> is replaced by that URL's HTML fragment every few
// seconds while the tab is visible. A signed-out session (the gate's
// redirect or 401) or a missing partial stops it; a failed fetch keeps the last content and tries again.
for (const box of document.querySelectorAll<HTMLElement>("[data-poll]")) {
	const url = box.dataset.poll ?? "";
	const delay = Number(box.dataset.pollSeconds ?? "5") * 1000;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let stopped = false;

	const schedule = () => {
		clearTimeout(timer);
		if (!stopped && document.visibilityState === "visible") timer = setTimeout(refresh, delay);
	};
	const refresh = async () => {
		try {
			const response = await fetch(url, { cache: "no-store", redirect: "manual" });
			if (response.ok) {
				box.innerHTML = await response.text();
				box.dataset.updated = new Date().toISOString();
			} else if (
				response.type === "opaqueredirect" ||
				response.status === 401 ||
				response.status === 404
			) {
				stopped = true;
			}
		} catch {
			// Offline or slow: keep what is shown.
		}
		schedule();
	};

	document.addEventListener("visibilitychange", () => {
		if (document.visibilityState === "visible") void refresh();
		else clearTimeout(timer);
	});
	schedule();
}
