// Behaviour of SearchForm: blueprint-type panel toggling, auto-submit on sort,
// and the canonical form of the submitted query. (Production rows validate
// themselves in ProductionFilters.svelte.)
import { canonicalFormParams } from "@lib/search/query";

const form = document.querySelector<HTMLFormElement>("#search-form");
function updateTypeFilters() {
	const type =
		form?.querySelector<HTMLInputElement>('input[name="blueprint_type"]:checked')?.value ??
		"all";
	for (const fields of form?.querySelectorAll<HTMLFieldSetElement>("[data-type-filters]") ?? []) {
		fields.hidden = type !== "all" && type !== fields.dataset.typeFilters;
		fields.disabled = fields.hidden;
	}
}
form?.addEventListener("change", (event) => {
	if (event.target instanceof HTMLInputElement && event.target.name === "blueprint_type")
		updateTypeFilters();
});
form?.addEventListener("reset", () => window.setTimeout(updateTypeFilters, 0));
form?.querySelector<HTMLSelectElement>("#search-sort")?.addEventListener("change", () => {
	form.requestSubmit();
});

form?.addEventListener("input", (event) => {
	if (event.target instanceof HTMLInputElement) event.target.setCustomValidity("");
});

form?.addEventListener("formdata", (event) => {
	// The URL is the canonical query (lib/search/query.ts): defaults left out.
	const canonical = canonicalFormParams(event.formData);
	for (const name of new Set(event.formData.keys())) event.formData.delete(name);
	for (const [name, value] of canonical) event.formData.append(name, value);
});
