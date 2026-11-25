// StringTool: decode fills the JSON field (pretty-printed), encode fills the
// string field. JSON that does not parse is reported here, before any call.
// Also the JSON field's Edit / Tree tabs and each field's copy and clear
// buttons (FieldTools).
import { browserApi, runAction, type ActionOptions } from "@lib/browser-action";
import { bindCopyButton } from "@lib/copy-button";
import { bindDragString } from "@lib/drag-string";
import { bindFileDrop } from "@lib/file-drop";
import { setStatus } from "@lib/status";

for (const form of document.querySelectorAll<HTMLFormElement>("[data-string-tool]")) {
	const fields = form.querySelector<HTMLFieldSetElement>("[data-string-tool-fields]")!;
	const status = form.querySelector<HTMLParagraphElement>("[data-string-tool-status]")!;
	const exchange = form.querySelector<HTMLTextAreaElement>('[name="blueprint"]')!;
	const json = form.querySelector<HTMLTextAreaElement>('[name="json"]')!;
	const decode = form.querySelector<HTMLButtonElement>("[data-string-tool-decode]")!;
	const encode = form.querySelector<HTMLButtonElement>("[data-string-tool-encode]")!;
	const options: Omit<ActionOptions, "fallback"> = {
		status,
		controls: [fields],
		restore: true,
		onUnauthorized: "clear",
	};
	// The JSON tree island listens for input events on the JSON field.
	const setJson = (text: string) => {
		json.value = text;
		json.dispatchEvent(new Event("input"));
	};

	// Dropped files fill either field; the JSON tree follows its input event.
	for (const field of [exchange, json])
		bindFileDrop(field, (message) => setStatus(status, message));

	decode.addEventListener("click", async () => {
		if (!exchange.value.trim()) {
			setStatus(status, "Paste an exchange string first.");
			return;
		}
		const payload = await runAction(
			{ ...options, busy: "Decoding...", fallback: "Could not decode the string." },
			() => browserApi().blueprints.decode(exchange.value.trim()),
		);
		if (payload === undefined) return;
		setJson(JSON.stringify(payload, null, 2));
		setStatus(status, "Decoded.");
	});

	encode.addEventListener("click", async () => {
		if (!json.value.trim()) {
			setStatus(status, "Paste or write JSON first.");
			return;
		}
		let payload: unknown;
		try {
			payload = JSON.parse(json.value);
		} catch (err) {
			setStatus(
				status,
				`The JSON does not parse: ${err instanceof Error ? err.message : "invalid JSON"}`,
			);
			return;
		}
		if (typeof payload !== "object" || payload === null || Array.isArray(payload)) {
			setStatus(status, "The JSON must be an object: a blueprint, book or planner envelope.");
			return;
		}
		const encoded = await runAction(
			{ ...options, busy: "Encoding...", fallback: "Could not encode the JSON." },
			() => browserApi().blueprints.encode({ ...payload }),
		);
		if (encoded === undefined) return;
		exchange.value = encoded;
		setStatus(status, "Encoded.");
	});

	// Edit / Tree tabs (arrow keys, Home and End move between them).
	const tabs = [...form.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
	const selectTab = (selected: HTMLButtonElement) => {
		for (const tab of tabs) {
			const active = tab === selected;
			tab.setAttribute("aria-selected", String(active));
			tab.tabIndex = active ? 0 : -1;
			document.getElementById(tab.getAttribute("aria-controls")!)!.hidden = !active;
		}
	};
	for (const [index, tab] of tabs.entries()) {
		tab.addEventListener("click", () => selectTab(tab));
		tab.addEventListener("keydown", (event) => {
			if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
			event.preventDefault();
			const next =
				event.key === "Home"
					? 0
					: event.key === "End"
						? tabs.length - 1
						: (index + (event.key === "ArrowRight" ? 1 : -1) + tabs.length) %
							tabs.length;
			selectTab(tabs[next]);
			tabs[next].focus();
		});
	}

	for (const tools of form.querySelectorAll<HTMLElement>("[data-field-tools]")) {
		const field = form.querySelector<HTMLTextAreaElement>(`#${tools.dataset.fieldTools}`)!;
		const copy = tools.querySelector<HTMLButtonElement>("[data-field-copy]")!;
		const clear = tools.querySelector<HTMLButtonElement>("[data-field-clear]")!;
		bindCopyButton(
			copy,
			field,
			tools.querySelector<HTMLElement>("[data-field-copy-status]")!,
			copy.getAttribute("aria-label")!,
		);
		const handle = tools.querySelector<HTMLElement>("[data-drag-string]");
		if (handle) bindDragString(handle, field);
		clear.addEventListener("click", () => {
			if (field === json) setJson("");
			else field.value = "";
			setStatus(status, "");
			// The tree replaces the JSON field while its tab is shown.
			if (!field.closest("[hidden]")) field.focus();
		});
		copy.disabled = false;
		clear.disabled = false;
	}

	// Rendered disabled until these handlers exist.
	decode.disabled = false;
	encode.disabled = false;
}
