# Security policy

## Reporting a vulnerability

Please report security problems privately, not in a public issue or pull request.

- Use GitHub's **Report a vulnerability** button on this repository's
  *Security* tab (private vulnerability reporting).
- For the hosted site, the contact address is also published at
  `/.well-known/security.txt` on the site itself.

Include what you found, the steps or request that show it, and the commit or
site page you tested. You will get an acknowledgement within a few days. This
is a volunteer project: please allow a reasonable time to fix a problem before
you disclose it, and we will credit you if you want that.

## Scope

In scope: the code in this repository and the hosted site that runs it, for
example authentication and sessions, edit tokens, access to private entries,
injection, SSRF through import from link, the staff API and console, and
anything that exposes another user's data.

Out of scope: denial of service by traffic volume, social engineering, findings
that need a compromised host, and automated scanner output without a working
proof. Please do not test against other people's accounts or data, and do not
upload content you do not have the right to share.

## Deployment notes

Secrets are never committed. `.env.example` lists every setting with
placeholders, and the API refuses to start with the placeholder secrets.
