import { createLocalJWKSet, exportJWK, generateKeyPair, SignJWT, type JWTPayload } from "jose";
import { beforeAll, describe, expect, it } from "vitest";

import {
	accessLogoutUrl,
	ACCESS_JWT_HEADER,
	createAccessVerifier,
	isLocalOrigin,
	normalizeTeamDomain,
} from "./access";

const team = "team.cloudflareaccess.com";
const audience = "aud-tag-1";

let privateKey: CryptoKey;
let otherKey: CryptoKey;
let verify: ReturnType<typeof createAccessVerifier>;

beforeAll(async () => {
	const pair = await generateKeyPair("RS256");
	privateKey = pair.privateKey;
	otherKey = (await generateKeyPair("RS256")).privateKey;
	const jwk = { ...(await exportJWK(pair.publicKey)), kid: "k1", alg: "RS256", use: "sig" };
	verify = createAccessVerifier(
		{ teamDomain: team, audience },
		createLocalJWKSet({ keys: [jwk] }),
	);
});

async function token(
	claims: JWTPayload,
	options: {
		key?: CryptoKey;
		issuer?: string;
		audience?: string;
		expires?: string | number;
	} = {},
) {
	return new SignJWT(claims)
		.setProtectedHeader({ alg: "RS256", kid: "k1" })
		.setIssuer(options.issuer ?? `https://${team}`)
		.setAudience(options.audience ?? audience)
		.setIssuedAt()
		.setExpirationTime(options.expires ?? "10m")
		.sign(options.key ?? privateKey);
}

const headers = (jwt?: string) => new Headers(jwt ? { [ACCESS_JWT_HEADER]: jwt } : {});

describe("createAccessVerifier", () => {
	it("names the staff member by the email claim", async () => {
		const result = await verify(headers(await token({ email: "owner@example.test" })));
		expect(result).toEqual({ ok: true, username: "owner@example.test" });
	});

	it("refuses a request without the token", async () => {
		const result = await verify(headers());
		expect(result).toMatchObject({ ok: false });
	});

	it("refuses an expired token", async () => {
		const jwt = await token(
			{ email: "owner@example.test" },
			{ expires: Math.floor(Date.now() / 1000) - 120 },
		);
		expect(await verify(headers(jwt))).toEqual({ ok: false, reason: "token expired" });
	});

	it("refuses another application's audience", async () => {
		const jwt = await token({ email: "owner@example.test" }, { audience: "someone-else" });
		expect(await verify(headers(jwt))).toMatchObject({
			ok: false,
			reason: expect.stringContaining("aud"),
		});
	});

	it("refuses another issuer", async () => {
		const jwt = await token(
			{ email: "owner@example.test" },
			{ issuer: "https://evil.example" },
		);
		expect(await verify(headers(jwt))).toMatchObject({
			ok: false,
			reason: expect.stringContaining("iss"),
		});
	});

	it("refuses a token signed with another key", async () => {
		const jwt = await token({ email: "owner@example.test" }, { key: otherKey });
		expect(await verify(headers(jwt))).toEqual({
			ok: false,
			reason: "token signature invalid",
		});
	});

	it("refuses an unsigned token", async () => {
		const unsigned = [
			Buffer.from('{"alg":"none"}').toString("base64url"),
			Buffer.from(
				JSON.stringify({ email: "a@b.test", iss: `https://${team}`, aud: audience }),
			).toString("base64url"),
			"",
		].join(".");
		expect(await verify(headers(unsigned))).toMatchObject({ ok: false });
	});

	it("refuses garbage", async () => {
		expect(await verify(headers("not-a-jwt"))).toMatchObject({ ok: false });
	});

	it("refuses a token without an email (a service token)", async () => {
		const jwt = await token({ common_name: "service" });
		expect(await verify(headers(jwt))).toEqual({
			ok: false,
			reason: "token has no email claim",
		});
	});

	it("refuses an email longer than the staff username limit", async () => {
		const jwt = await token({ email: `${"a".repeat(60)}@example.test` });
		expect(await verify(headers(jwt))).toMatchObject({ ok: false });
	});
});

describe("team domain", () => {
	it("is reduced to the bare host", () => {
		expect(normalizeTeamDomain(" https://team.cloudflareaccess.com/ ")).toBe(team);
	});
	it("gives the logout page", () => {
		expect(accessLogoutUrl(team)).toBe(
			"https://team.cloudflareaccess.com/cdn-cgi/access/logout",
		);
	});
});

describe("isLocalOrigin", () => {
	it("accepts development hosts only", () => {
		expect(isLocalOrigin("http://localhost:3200")).toBe(true);
		expect(isLocalOrigin("https://expo.localhost:8443")).toBe(true);
		expect(isLocalOrigin("http://127.0.0.1:3200")).toBe(true);
		expect(isLocalOrigin("http://[::1]:3200")).toBe(true);
		expect(isLocalOrigin("https://admin.example.org")).toBe(false);
		expect(isLocalOrigin("https://localhost.evil.test")).toBe(false);
		expect(isLocalOrigin("not a url")).toBe(false);
	});
});
