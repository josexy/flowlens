import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("../../", import.meta.url));

const args = process.argv.slice(2);
let inputVersion = process.env.VERSION || "";
let checkOnly = false;

for (let index = 0; index < args.length; index += 1) {
  const arg = args[index];
  if (arg === "--check") {
    checkOnly = true;
    continue;
  }
  if (arg === "--version") {
    inputVersion = args[index + 1] || "";
    index += 1;
    continue;
  }
  if (!inputVersion) {
    inputVersion = arg;
    continue;
  }
  throw new Error(`Unexpected argument: ${arg}`);
}

const versionMatch = inputVersion
  .trim()
  .match(/^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/);

if (!versionMatch) {
  throw new Error(
    `Version must be a stable SemVer value like 1.2.3 or v1.2.3, got: ${inputVersion || "<empty>"}`,
  );
}

const displayVersion = `${versionMatch[1]}.${versionMatch[2]}.${versionMatch[3]}`;
const windowsFixedVersion = `${displayVersion}.0`;

function resolvePath(relativePath) {
  return path.join(repoRoot, relativePath);
}

function readText(relativePath) {
  return fs.readFileSync(resolvePath(relativePath), "utf8");
}

function writeText(relativePath, text) {
  fs.writeFileSync(resolvePath(relativePath), text, "utf8");
}

function writeJson(relativePath, value, originalText) {
  const newline = originalText.includes("\r\n") ? "\r\n" : "\n";
  const text = `${JSON.stringify(value, null, 2)}\n`.replaceAll("\n", newline);
  writeText(relativePath, text);
}

function assertEqual(relativePath, description, actual, expected) {
  if (actual !== expected) {
    throw new Error(
      `${relativePath}: ${description} is ${JSON.stringify(actual)}, expected ${JSON.stringify(expected)}`,
    );
  }
}

function logOk(relativePath, description) {
  console.log(`OK ${relativePath}: ${description}`);
}

function logUpdated(relativePath, description) {
  console.log(`Updated ${relativePath}: ${description}`);
}

function updateTextVersion(
  relativePath,
  pattern,
  expectedVersion,
  description,
  options = {},
) {
  const text = readText(relativePath);
  const matches = [...text.matchAll(pattern)];

  if (matches.length === 0) {
    if (options.optional) {
      console.warn(`Skipped ${relativePath}: ${description} was not found`);
      return;
    }
    throw new Error(`${relativePath}: ${description} was not found`);
  }

  if (checkOnly) {
    for (const match of matches) {
      assertEqual(
        relativePath,
        description,
        match.groups.version,
        expectedVersion,
      );
    }
    logOk(relativePath, description);
    return;
  }

  const updated = text.replace(pattern, `$<prefix>${expectedVersion}$<suffix>`);
  if (updated !== text) {
    writeText(relativePath, updated);
  }
  logUpdated(relativePath, description);
}

function updatePackageJson(relativePath) {
  const text = readText(relativePath);
  const json = JSON.parse(text);

  if (checkOnly) {
    assertEqual(relativePath, "package version", json.version, displayVersion);
    logOk(relativePath, "package version");
    return;
  }

  json.version = displayVersion;
  writeJson(relativePath, json, text);
  logUpdated(relativePath, "package version");
}

function updatePackageLock(relativePath) {
  const text = readText(relativePath);
  const json = JSON.parse(text);

  if (!json.packages || !json.packages[""]) {
    throw new Error(`${relativePath}: root package entry was not found`);
  }

  if (checkOnly) {
    assertEqual(relativePath, "root version", json.version, displayVersion);
    assertEqual(
      relativePath,
      "packages root version",
      json.packages[""].version,
      displayVersion,
    );
    logOk(relativePath, "root package versions");
    return;
  }

  json.version = displayVersion;
  json.packages[""].version = displayVersion;
  writeJson(relativePath, json, text);
  logUpdated(relativePath, "root package versions");
}

function updateWindowsInfo(relativePath) {
  const text = readText(relativePath);
  const json = JSON.parse(text);

  if (!json.fixed || !json.info || !json.info["0000"]) {
    throw new Error(
      `${relativePath}: expected fixed/info metadata was not found`,
    );
  }

  if (checkOnly) {
    assertEqual(
      relativePath,
      "fixed file_version",
      json.fixed.file_version,
      windowsFixedVersion,
    );
    assertEqual(
      relativePath,
      "fixed product_version",
      json.fixed.product_version,
      windowsFixedVersion,
    );
    assertEqual(
      relativePath,
      "FileVersion",
      json.info["0000"].FileVersion,
      displayVersion,
    );
    assertEqual(
      relativePath,
      "ProductVersion",
      json.info["0000"].ProductVersion,
      displayVersion,
    );
    logOk(relativePath, "Windows version metadata");
    return;
  }

  json.fixed.file_version = windowsFixedVersion;
  json.fixed.product_version = windowsFixedVersion;
  json.info["0000"].FileVersion = displayVersion;
  json.info["0000"].ProductVersion = displayVersion;
  writeJson(relativePath, json, text);
  logUpdated(relativePath, "Windows version metadata");
}

updateTextVersion(
  "build/config.yml",
  /^(?<prefix>\s{2}version:\s*["'])(?<version>[^"']+)(?<suffix>["']\s*)$/gm,
  displayVersion,
  "Wails app version",
);

updateTextVersion(
  "backend/services/app_service/app_service.go",
  /^(?<prefix>\s*APP_VERSION\s*=\s*")(?<version>[^"]+)(?<suffix>"\s*)$/gm,
  displayVersion,
  "Go app version",
);

updatePackageJson("frontend/package.json");
updatePackageLock("frontend/package-lock.json");
updateWindowsInfo("build/windows/info.json");

updateTextVersion(
  "build/windows/wails.exe.manifest",
  /(?<prefix><assemblyIdentity\b(?=[^>]*\bname="com\.josexy\.flowlens")[^>]*\bversion=")(?<version>[^"]+)(?<suffix>")/g,
  displayVersion,
  "Windows app manifest version",
);

updateTextVersion(
  "build/windows/nsis/wails_tools.nsh",
  /^(?<prefix>\s*!define\s+INFO_PRODUCTVERSION\s+")(?<version>[^"]+)(?<suffix>"\s*)$/gm,
  displayVersion,
  "NSIS product version",
);

updateTextVersion(
  "build/linux/nfpm/nfpm.yaml",
  /^(?<prefix>version:\s*["']?)(?<version>[^"'\s]+)(?<suffix>["']?\s*)$/gm,
  displayVersion,
  "Linux package version",
);

for (const plistPath of [
  "build/darwin/Info.plist",
  "build/darwin/Info.dev.plist",
]) {
  updateTextVersion(
    plistPath,
    /(?<prefix><key>CFBundleVersion<\/key>\s*<string>)(?<version>[^<]+)(?<suffix><\/string>)/g,
    displayVersion,
    "CFBundleVersion",
  );
  updateTextVersion(
    plistPath,
    /(?<prefix><key>CFBundleShortVersionString<\/key>\s*<string>)(?<version>[^<]+)(?<suffix><\/string>)/g,
    displayVersion,
    "CFBundleShortVersionString",
  );
}

const mode = checkOnly ? "matches" : "updated to";
console.log(`Release version metadata ${mode} ${displayVersion}.`);
