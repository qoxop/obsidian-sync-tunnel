import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, "..");
const argumentsFromCommandLine = process.argv.slice(2);
const expectedTag = argumentsFromCommandLine.find((argument) => argument !== "--artifacts") ?? "";
const checkArtifacts = argumentsFromCommandLine.includes("--artifacts");

function fail(message) {
  console.error(`Release validation failed: ${message}`);
  process.exit(1);
}

function readText(relativePath) {
  return fs.readFileSync(path.join(repositoryRoot, relativePath), "utf8").replaceAll("\r\n", "\n");
}

function readJson(relativePath) {
  return JSON.parse(readText(relativePath));
}

const rootManifestText = readText("manifest.json");
const pluginManifestText = readText("plugin/manifest.json");
if (rootManifestText !== pluginManifestText) {
  fail("manifest.json and plugin/manifest.json must be identical");
}

const rootVersionsText = readText("versions.json");
const pluginVersionsText = readText("plugin/versions.json");
if (rootVersionsText !== pluginVersionsText) {
  fail("versions.json and plugin/versions.json must be identical");
}

const manifest = JSON.parse(rootManifestText);
const packageJson = readJson("plugin/package.json");
const packageLock = readJson("plugin/package-lock.json");
const versions = JSON.parse(rootVersionsText);
const semanticVersion = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:(?:0|[1-9]\d*|[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|[A-Za-z-][0-9A-Za-z-]*))*))?$/u;

if (!/^[a-z0-9-]+$/u.test(manifest.id) || manifest.id.includes("obsidian") || manifest.id.endsWith("plugin")) {
  fail(`invalid Obsidian plugin ID '${manifest.id}'`);
}
if (!semanticVersion.test(manifest.version)) {
  fail(`manifest version '${manifest.version}' must use semantic x.y.z or x.y.z-prerelease format`);
}
if (packageJson.version !== manifest.version || packageLock.version !== manifest.version || packageLock.packages?.[""]?.version !== manifest.version) {
  fail("plugin package versions must match manifest.json");
}
if (packageJson.name !== manifest.id || packageLock.name !== manifest.id || packageLock.packages?.[""]?.name !== manifest.id) {
  fail("plugin package names must match the manifest ID");
}
if (versions[manifest.version] !== manifest.minAppVersion) {
  fail(`versions.json must map ${manifest.version} to ${manifest.minAppVersion}`);
}
if (expectedTag && expectedTag !== manifest.version) {
  fail(`Git tag '${expectedTag}' must exactly match manifest version '${manifest.version}'`);
}

if (checkArtifacts) {
  for (const relativePath of ["plugin/main.js", "plugin/manifest.json", "plugin/styles.css"]) {
    const absolutePath = path.join(repositoryRoot, relativePath);
    if (!fs.existsSync(absolutePath) || fs.statSync(absolutePath).size === 0) {
      fail(`missing or empty release artifact: ${relativePath}`);
    }
  }
  const pluginBundle = readText("plugin/main.js");
  if (/import\s*\(\s*["']node:/u.test(pluginBundle)) {
    fail("plugin bundle contains a dynamic node: import, which Obsidian blocks from app:// pages");
  }
}

console.log(`Plugin release metadata is valid for ${manifest.id} ${manifest.version}.`);
