/** The parts of an element a status line uses. */
export type StatusElement = Pick<HTMLElement, "textContent" | "setAttribute" | "removeAttribute">;

/** Sets a status line; `busy` shows Pico's spinner (aria-busy) before the text. */
export function setStatus(element: StatusElement, text: string, busy = false) {
	element.textContent = text;
	if (busy) element.setAttribute("aria-busy", "true");
	else element.removeAttribute("aria-busy");
}
