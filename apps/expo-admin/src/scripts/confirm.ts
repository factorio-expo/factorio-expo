// Buttons that do something hard to undo ask first:
// <button data-confirm="Delete this entry?">. Declining cancels the submit.
document.addEventListener("submit", (event) => {
	const button = event.submitter;
	const message = button instanceof HTMLElement ? button.dataset.confirm : undefined;
	if (message && !window.confirm(message)) event.preventDefault();
});
