// Drag handles for exchange strings: dragging one carries the whole string as
// text, which Factorio accepts when dropped on its window. A span, not a
// button, because Firefox does not start drags from buttons. Import only from
// client-side code.

const IMAGE_WIDTH = 160;

/**
 * The page's share image (a spoiler entry has none, so it is the site icon),
 * loaded offscreen on first use so it is decoded by the time a drag starts.
 */
let dragImage: HTMLImageElement | undefined;
function preloadDragImage() {
	if (dragImage) return;
	const src = document.querySelector<HTMLMetaElement>('meta[property="og:image"]')?.content;
	if (!src) return;
	dragImage = new Image();
	dragImage.width = IMAGE_WIDTH;
	dragImage.style.cssText = "position:fixed;top:-9999px;left:-9999px;pointer-events:none";
	dragImage.src = src;
	document.body.append(dragImage);
}

/** Makes `handle` drag the current text of `source` (a string field or a getter). */
export function bindDragString(handle: HTMLElement, source: HTMLTextAreaElement | (() => string)) {
	const text = () => (typeof source === "function" ? source() : source.value);
	handle.draggable = true;
	for (const type of ["pointerenter", "focus"]) handle.addEventListener(type, preloadDragImage);
	handle.addEventListener("dragstart", (event) => {
		const value = text().trim();
		if (!value || !event.dataTransfer) {
			event.preventDefault();
			return;
		}
		event.dataTransfer.effectAllowed = "copy";
		event.dataTransfer.setData("text/plain", value);
		if (dragImage?.complete && dragImage.naturalWidth > 0) {
			event.dataTransfer.setDragImage(dragImage, dragImage.width / 2, dragImage.height / 2);
		}
	});
}
