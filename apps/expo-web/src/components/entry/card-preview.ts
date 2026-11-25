// Entry card preview boxes: Pico's spinner (aria-busy) while the image loads,
// and data-failed when it fails, so EntryCard.astro shows the site icon
// instead of a broken image. Set from here, not the server, so a page without
// scripts shows the image as it arrives. load and error do not bubble, so
// they are caught on the way down; images done before this module ran are
// settled once. A spoiler card's preview stays blurred until its first click.

const BOX = "[data-card-preview]";

function settle(image: HTMLImageElement) {
	const box = image.closest<HTMLElement>(BOX);
	if (!box) return;
	if (!image.complete) {
		box.setAttribute("aria-busy", "true");
		return;
	}
	box.removeAttribute("aria-busy");
	if (image.naturalWidth === 0) box.dataset.failed = "";
}

for (const type of ["load", "error"]) {
	document.addEventListener(
		type,
		(event) => {
			if (event.target instanceof HTMLImageElement) settle(event.target);
		},
		true,
	);
}
for (const image of document.querySelectorAll<HTMLImageElement>(`${BOX} img`)) settle(image);

// A spoiler card's first click on its preview reveals the image instead of
// following the link; the title and a second click still open the entry.
document.addEventListener("click", (event) => {
	if (!(event.target instanceof Element)) return;
	const box = event.target.closest<HTMLElement>(`${BOX}[data-spoiler]:not([data-revealed])`);
	if (!box) return;
	event.preventDefault();
	box.dataset.revealed = "";
});
