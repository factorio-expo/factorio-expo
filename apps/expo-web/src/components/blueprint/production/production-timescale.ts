// Rate display switcher for ProductionAnalysis: rewrites every
// [data-production-rate] value when the rate unit segment changes (also on
// analysis markup swapped in once it arrives).
import { onLiveRegions } from "@lib/live-regions";
import { formatPower, formatRate, productionTimescales } from "@utils/production.ts";

onLiveRegions((scope) => {
	for (const section of scope.querySelectorAll<HTMLElement>("[data-production]")) bind(section);
});

function bind(section: HTMLElement) {
	const units = section.querySelector<HTMLFieldSetElement>("[data-production-timescale]");
	const factorInput = section.querySelector<HTMLInputElement>("[data-production-factor]");
	const namesButton = section.querySelector<HTMLButtonElement>("[data-names-toggle]");
	const note = section.querySelector<HTMLElement>("[data-belt-note]");
	function updateRates() {
		const chosen = units?.querySelector<HTMLInputElement>("input:checked")?.value;
		const timescale = productionTimescales.find((scale) => scale.value === chosen);
		if (!timescale) return;
		const parsed = Number(factorInput?.value);
		const factor =
			factorInput?.value !== "" && Number.isFinite(parsed) && parsed >= 0 ? parsed : 1;
		for (const rate of section.querySelectorAll<HTMLElement>("[data-production-rate]")) {
			const ratio = rate.hasAttribute("data-rate-ratio");
			rate.textContent = formatRate(
				Number(rate.dataset.productionRate),
				timescale,
				rate.dataset.rateType,
				factor,
				ratio,
			);
		}
		for (const power of section.querySelectorAll<HTMLElement>("[data-production-power]")) {
			power.textContent = formatPower(Number(power.dataset.productionPower) * factor);
		}
		if (note) note.hidden = !timescale.belt;
	}
	namesButton?.addEventListener("click", (event) => {
		// The button lives in the collapsible summary; don't toggle the section.
		event.preventDefault();
		const on = namesButton.getAttribute("aria-pressed") !== "true";
		namesButton.setAttribute("aria-pressed", String(on));
		const label = on ? "Hide names" : "Show names";
		namesButton.dataset.tooltip = label;
		namesButton.setAttribute("aria-label", label);
		section.toggleAttribute("data-names", on);
	});
	units?.addEventListener("change", updateRates);
	factorInput?.addEventListener("input", updateRates);
	updateRates();
}
