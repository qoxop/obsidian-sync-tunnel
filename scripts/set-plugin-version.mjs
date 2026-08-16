import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const version = process.argv[2] ?? "";
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u.test(version)) {
  console.error("Usage: node scripts/set-plugin-version.mjs <x.y.z>");
  process.exit(1);
}

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..");

function readJson(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(repositoryRoot, relativePath), "utf8"));
}

function writeJson(relativePath, value) {
  fs.writeFileSync(path.join(repositoryRoot, relativePath), `${JSON.stringify(value, null, 2)}\n`, "utf8");
}

const manifest = readJson("manifest.json");
manifest.version = version;
writeJson("manifest.json", manifest);
writeJson("plugin/manifest.json", manifest);

const versions = readJson("versions.json");
versions[version] = manifest.minAppVersion;
writeJson("versions.json", versions);
writeJson("plugin/versions.json", versions);

const packageJson = readJson("plugin/package.json");
packageJson.version = version;
writeJson("plugin/package.json", packageJson);

const packageLock = readJson("plugin/package-lock.json");
packageLock.version = version;
packageLock.packages[""].version = version;
writeJson("plugin/package-lock.json", packageLock);

console.log(`Plugin version updated to ${version}. Review and commit the changed files before tagging.`);
