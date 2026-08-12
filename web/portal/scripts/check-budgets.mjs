import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

const output = resolve(process.cwd(), "../../internal/platformctl/portalassets");
const names = readdirSync(output).filter(name => name !== "asset-manifest.json").sort();
const files = names.map(name => {
  const content = readFileSync(resolve(output, name));
  return { name, bytes: content.length, gzipBytes: gzipSync(content, { level: 9, mtime: 0 }).length, sha256: createHash("sha256").update(content).digest("hex") };
});
const javascript = files.filter(file => file.name.endsWith(".js")).reduce((sum, file) => sum + file.gzipBytes, 0);
const css = files.filter(file => file.name.endsWith(".css")).reduce((sum, file) => sum + file.gzipBytes, 0);
if (javascript > 250 * 1024) throw new Error(`Compressed JavaScript budget exceeded: ${javascript} bytes`);
if (css > 80 * 1024) throw new Error(`Compressed CSS budget exceeded: ${css} bytes`);
const manifest = { schemaVersion: "portal.assets.steadystate.dev/v1alpha1", files, budgets: { javascriptGzipBytes: javascript, javascriptLimitBytes: 250 * 1024, cssGzipBytes: css, cssLimitBytes: 80 * 1024 } };
writeFileSync(resolve(output, "asset-manifest.json"), `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
