// Applies the saved/preferred theme before first paint. A static file rather
// than an inline script so the page CSP needs no inline-script allowance.
(() => {
	let savedTheme = null;
	try {
		savedTheme = localStorage.getItem("theme");
	} catch {
		// Storage unavailable: fall back to the system preference.
	}
	const prefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
	document.documentElement.dataset.theme =
		savedTheme === "light" || savedTheme === "dark"
			? savedTheme
			: prefersDark
				? "dark"
				: "light";
})();
