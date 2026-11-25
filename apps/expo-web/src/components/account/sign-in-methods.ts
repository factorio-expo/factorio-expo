// Account settings: add a passkey, remove a sign-in method, link a provider
// (the list itself is server-rendered; a change reloads it), download the
// account's data, make a new recovery code and delete the account (after its own dialog, where the
// person types their username). Each needs a recent login: when the API asks
// for one, the "Confirm it's you" dialog logs in again (lib/fresh-sign-in.ts)
// and the change is retried.
import { getBrowserApiClient } from "@lib/api/browser";
import { runAction } from "@lib/browser-action";
import { bindDialog } from "@lib/dialog";
import {
	isReauthRequired,
	rememberPendingChange,
	takePendingChange,
	type PendingChange,
} from "@lib/fresh-sign-in";
import { addPasskey, confirmWithPasskey, passkeysSupported } from "@lib/passkeys";
import { destinationAfterSignIn } from "@lib/recovery-code";
import { signInErrorText } from "@lib/sign-in-errors";
import { setStatus } from "@lib/status";

/** The changes made with an API call (linking is a navigation). */
type ApiChange = Exclude<PendingChange, { kind: "link" }>;

const root = document.querySelector<HTMLElement>("[data-sign-in-methods]");

/** Saves a downloaded file under `filename`. */
const save = (blob: Blob, filename: string) => {
	const url = URL.createObjectURL(blob);
	const link = document.createElement("a");
	link.href = url;
	link.download = filename;
	document.body.append(link);
	link.click();
	link.remove();
	// Revoked later: the browser may still be reading it.
	setTimeout(() => URL.revokeObjectURL(url), 60_000);
};

if (root) {
	const status = root.querySelector<HTMLElement>("[data-methods-status]")!;
	const addButton = root.querySelector<HTMLButtonElement>("[data-add-passkey]")!;
	// "Your data": rendered with the sign-in methods, outside their section.
	const exportButton = document.querySelector<HTMLButtonElement>("[data-export]")!;
	const exportStatus = document.querySelector<HTMLElement>("[data-export-status]")!;
	// "Recovery code": also outside the section.
	const codeButton = document.querySelector<HTMLButtonElement>("[data-new-recovery-code]")!;
	const codeStatus = document.querySelector<HTMLElement>("[data-recovery-code-status]")!;
	// "Delete account": also outside, with its own dialog.
	const deleteSection = document.querySelector<HTMLElement>("[data-delete-account]")!;
	const deleteOpen = deleteSection.querySelector<HTMLButtonElement>("[data-delete-open]")!;
	const deleteStatus = deleteSection.querySelector<HTMLElement>("[data-delete-status]")!;
	const deleteDialog = deleteSection.querySelector<HTMLDialogElement>("dialog")!;
	const deleteForm = deleteDialog.querySelector<HTMLFormElement>("[data-delete-form]")!;
	const deleteUsername = deleteForm.querySelector<HTMLInputElement>("[data-delete-username]")!;
	const deleteConfirm = deleteForm.querySelector<HTMLButtonElement>("[data-delete-confirm]")!;
	const dialog = root.querySelector<HTMLDialogElement>("dialog")!;
	const confirmStatus = dialog.querySelector<HTMLElement>("[data-confirm-status]")!;
	const confirmPasskey = dialog.querySelector<HTMLButtonElement>("[data-confirm-passkey]");
	// The page's own controls; the dialog keeps its buttons while a change runs.
	const buttons = () =>
		[...root.querySelectorAll<HTMLButtonElement>("button")].filter((b) => !b.closest("dialog"));
	const linkFor = (provider: string) =>
		root.querySelector<HTMLAnchorElement>(
			`[data-link-providers] a[data-provider="${CSS.escape(provider)}"]`,
		);
	const api = getBrowserApiClient();

	/** Shows "Confirm it's you", with `message` (a refused confirmation) if any. */
	const openConfirm = (message = "") => {
		setStatus(status, "");
		setStatus(exportStatus, "");
		setStatus(codeStatus, "");
		setStatus(deleteStatus, "");
		setStatus(confirmStatus, message);
		if (!dialog.open) dialog.showModal();
	};

	const downloadData = async () => {
		const blob = await api.auth.exportData();
		const day = new Date().toISOString().slice(0, 10);
		save(blob, `factorio-expo-${exportButton.dataset.username}-${day}.json`);
	};

	// Where the new recovery code is shown, once the API has made it.
	let codePage = "";
	const newRecoveryCode = async () => {
		const made = await api.auth.newRecoveryCode();
		codePage = destinationAfterSignIn(
			{ username: exportButton.dataset.username!, recovery_code: made.recovery_code },
			"regenerate",
			"/account",
		);
	};

	const call = (change: ApiChange): Promise<void> =>
		change.kind === "recovery-code"
			? newRecoveryCode()
			: change.kind === "remove-identity"
				? api.auth.removeIdentity(change.provider)
				: change.kind === "remove-passkey"
					? api.auth.removePasskey(change.id)
					: change.kind === "add-passkey"
						? addPasskey(api)
						: change.kind === "delete-account"
							? api.auth.deleteAccount()
							: downloadData();

	const removalRefused = {
		409: "Your only way to log in cannot be removed. Add another one first.",
	};
	const texts: Record<
		ApiChange["kind"],
		{ fallback: string; busy: string; messages?: Partial<Record<number, string>> }
	> = {
		"remove-identity": {
			fallback: "Could not remove it.",
			busy: "Removing...",
			messages: removalRefused,
		},
		"remove-passkey": {
			fallback: "Could not remove the passkey.",
			busy: "Removing...",
			messages: removalRefused,
		},
		"add-passkey": {
			fallback: "The passkey was cancelled or could not be added.",
			busy: "Waiting for your passkey...",
		},
		"recovery-code": {
			fallback: "Could not generate a recovery code. Try again.",
			busy: "Generating your recovery code...",
		},
		export: {
			fallback: "Your data could not be downloaded. Try again.",
			busy: "Preparing your data...",
			messages: { 429: "You downloaded your data a few times recently. Try again later." },
		},
		"delete-account": {
			fallback: "Your account could not be deleted. Try again.",
			busy: "Deleting your account...",
		},
	};

	/** Where each change shows its progress, and which controls it disables. */
	const placeOf = (kind: ApiChange["kind"]) =>
		kind === "export"
			? { status: exportStatus, controls: [exportButton] }
			: kind === "recovery-code"
				? { status: codeStatus, controls: [codeButton] }
				: kind === "delete-account"
					? { status: deleteStatus, controls: [deleteOpen] }
					: { status, controls: buttons() };

	const perform = async (change: PendingChange) => {
		if (change.kind === "link") {
			// The API checks the login and sends the browser to the provider, or
			// back here with ?error=reauth_required: keep the link for that.
			const link = linkFor(change.provider);
			rememberPendingChange(change);
			if (link) location.assign(link.href);
			return;
		}
		const outcome = await runAction(
			{
				...placeOf(change.kind),
				restore: true,
				...texts[change.kind],
				onUnauthorized: "login",
			},
			() =>
				call(change).then(
					() => "done" as const,
					(err: unknown) => {
						if (isReauthRequired(err)) return "reauth" as const;
						throw err;
					},
				),
		);
		if (outcome === "done" && change.kind === "export")
			setStatus(exportStatus, "Your data is downloaded.");
		else if (outcome === "done" && change.kind === "recovery-code") location.assign(codePage);
		// The API ended every session and cleared the cookie.
		else if (outcome === "done" && change.kind === "delete-account")
			location.assign("/account-deleted");
		else if (outcome === "done") location.reload();
		else if (outcome === "reauth") {
			rememberPendingChange(change);
			openConfirm();
		}
	};

	root.addEventListener("click", (event) => {
		if (!(event.target instanceof Element)) return;
		const link = event.target.closest<HTMLAnchorElement>(
			"[data-link-providers] a[data-provider]",
		);
		if (link) {
			event.preventDefault();
			void perform({ kind: "link", provider: link.dataset.provider! });
			return;
		}
		const button = event.target.closest<HTMLButtonElement>("button");
		if (!button || button.closest("dialog")) return;
		const provider = button.dataset.removeIdentity;
		const passkey = button.dataset.removePasskey;
		if (provider) void perform({ kind: "remove-identity", provider });
		else if (passkey) void perform({ kind: "remove-passkey", id: passkey });
		else if (button === addButton) void perform({ kind: "add-passkey" });
	});

	// "Confirm it's you" with a passkey happens in the page, then the change
	// is retried. A provider is a plain link (?reauth=1) that comes back with
	// ?reauth=done, handled below.
	confirmPasskey?.addEventListener("click", async () => {
		const confirmed = await runAction(
			{
				status: confirmStatus,
				controls: [confirmPasskey],
				restore: true,
				busy: "Waiting for your passkey...",
				fallback: "The passkey was cancelled or is not one of this account's.",
				onUnauthorized: "login",
			},
			() => confirmWithPasskey(api).then(() => true),
		);
		if (!confirmed) return;
		dialog.close();
		const change = takePendingChange();
		if (change) void perform(change);
	});
	exportButton.addEventListener("click", () => void perform({ kind: "export" }));
	codeButton.addEventListener("click", () => void perform({ kind: "recovery-code" }));

	// "Delete account" asks first: the dialog explains what goes, and the
	// button only works once the username is typed.
	const typedUsername = () =>
		deleteUsername.value.trim().toLowerCase() ===
		deleteUsername.dataset.expected!.toLowerCase();
	bindDialog({
		dialog: deleteDialog,
		opener: deleteOpen,
		// Opens at the top, so the explanation is read before the username
		// field (showModal focuses the close button in the header).
		onOpen: () => {
			deleteForm.reset();
			deleteConfirm.disabled = true;
			deleteDialog.querySelector("article")?.scrollTo({ top: 0 });
		},
	});
	deleteDialog
		.querySelector("[data-delete-cancel]")
		?.addEventListener("click", () => deleteDialog.close());
	deleteUsername.addEventListener("input", () => {
		deleteConfirm.disabled = !typedUsername();
	});
	deleteForm.addEventListener("submit", (event) => {
		event.preventDefault();
		if (!typedUsername()) return;
		deleteDialog.close();
		void perform({ kind: "delete-account" });
	});
	dialog.querySelector("[data-dialog-close]")?.addEventListener("click", () => dialog.close());
	dialog.addEventListener("click", (event) => {
		if (event.target === dialog) dialog.close();
	});

	// Rendered disabled (or hidden) until this handler exists; hidden without passkeys.
	if (passkeysSupported()) {
		addButton.disabled = false;
		if (confirmPasskey) confirmPasskey.hidden = false;
	} else {
		addButton.hidden = true;
	}

	// Back from a provider (the query goes, so a reload repeats nothing):
	// confirmed, so the waiting change runs; a stale login refused a link or
	// the confirmation was another account, so ask (again); anything else
	// (linked, or a failed link) ends the waiting link.
	const query = new URLSearchParams(location.search);
	const reauth = query.get("reauth");
	const error = query.get("error");
	if (reauth !== null || error !== null || query.has("linked")) {
		history.replaceState(null, "", location.pathname);
		const change = takePendingChange();
		if (reauth === "done") {
			if (change) void perform(change);
		} else if (error === "reauth_required" || error === "reauth-other-account") {
			if (change) rememberPendingChange(change);
			openConfirm(error === "reauth-other-account" ? signInErrorText(error) : "");
		}
	}
}
