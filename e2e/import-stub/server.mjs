// Stand-in for the sites the API imports blueprints from (e2e stack only: the
// API is pointed here by API_IMPORT_STUB_ORIGIN, each site under /<adapter>).
// Specs register the answers they need with PUT /__stub and read what was
// asked with GET /__log; there is no real internet in tests.
import http from "node:http";

const routes = new Map();
const log = [];

const readJson = async (req) => {
	const chunks = [];
	for await (const chunk of req) chunks.push(chunk);
	return JSON.parse(Buffer.concat(chunks).toString("utf8"));
};

http.createServer(async (req, res) => {
	const { pathname } = new URL(req.url, "http://stub");
	if (pathname === "/__stub" && req.method === "PUT") {
		const {
			path,
			status = 200,
			contentType = "text/plain; charset=utf-8",
			body = "",
		} = await readJson(req);
		routes.set(path, { status, contentType, body });
		res.writeHead(204).end();
	} else if (pathname === "/__log") {
		if (req.method === "DELETE") log.length = 0;
		res.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify(log));
	} else if (pathname === "/healthz") {
		res.writeHead(200).end("ok");
	} else {
		log.push(req.url);
		const route = routes.get(req.url);
		if (!route) {
			res.writeHead(404, { "Content-Type": "text/plain" }).end("not stubbed");
			return;
		}
		res.writeHead(route.status, { "Content-Type": route.contentType }).end(route.body);
	}
}).listen(Number(process.env.PORT ?? 8190), "0.0.0.0");
