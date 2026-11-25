// Shows the waiting recovery code (RecoveryCode.astro, lib/recovery-code.ts):
// copy it, download it as a text file, or continue to where the person was
// going. "Continue" forgets the code: after that nothing can show it again.
import { safeNext } from "@lib/auth";
import {
	forgetRecoveryCode,
	readRecoveryCode,
	recoveryCodeFile,
	recoveryCodeFileName,
} from "@lib/recovery-code";
import { setStatus } from "@lib/status";

const root = document.querySelector<HTMLElement>("[data-recovery]")!;
const part = (name: string) => root.querySelector<HTMLElement>(`[data-recovery-${name}]`)!;
const pending = readRecoveryCode();

part("loading").hidden = true;

if (!pending) {
	part("missing").hidden = false;
} else {
	part("shown").hidden = false;
	part("intro").textContent = {
		signup: "Your account is ready. Save your recovery code now.",
		recovery:
			"You are back in. The code you just used is void; this is your new recovery code. Next, add a new way to log in on your account page.",
		regenerate: "This is your new recovery code. Any earlier recovery code no longer works.",
	}[pending.reason];
	part("username").textContent = pending.username;
	part("code").textContent = pending.code;
	const status = part("status");

	part("copy").addEventListener("click", async () => {
		try {
			await navigator.clipboard.writeText(pending.code);
			setStatus(status, "Copied to clipboard.");
		} catch {
			setStatus(status, "Copy failed. Select the code and copy it by hand.");
		}
	});

	part("download").addEventListener("click", () => {
		const file = new Blob([recoveryCodeFile(pending, location.origin)], {
			type: "text/plain;charset=utf-8",
		});
		const link = document.createElement("a");
		link.href = URL.createObjectURL(file);
		link.download = recoveryCodeFileName(pending.username);
		link.click();
		URL.revokeObjectURL(link.href);
		setStatus(status, "Downloaded.");
	});

	part("continue").addEventListener("click", () => {
		forgetRecoveryCode();
		location.assign(safeNext(pending.next));
	});
}
