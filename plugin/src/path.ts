export function normalizeVaultPath(path: string): string {
  return path.replaceAll("\\", "/").replace(/^\.\//, "").replace(/^\/+/, "");
}

export function globMatches(path: string, pattern: string): boolean {
  const normalizedPath = normalizeVaultPath(path);
  const normalizedPattern = normalizeVaultPath(pattern.trim());
  if (!normalizedPattern) return false;

  let expression = "";
  for (let index = 0; index < normalizedPattern.length; index += 1) {
    const character = normalizedPattern[index];
    const next = normalizedPattern[index + 1];
    if (character === "*" && next === "*") {
      if (normalizedPattern[index + 2] === "/") {
        expression += "(?:.*/)?";
        index += 2;
      } else {
        expression += ".*";
        index += 1;
      }
    } else if (character === "*") {
      expression += "[^/]*";
    } else if (character === "?") {
      expression += "[^/]";
    } else {
      expression += escapeRegex(character ?? "");
    }
  }
  return new RegExp(`^${expression}$`, "u").test(normalizedPath);
}

function escapeRegex(character: string): string {
  return /[\\^$.*+?()[\]{}|]/u.test(character) ? `\\${character}` : character;
}

export function conflictPath(path: string, deviceId: string, date = new Date()): string {
  const slash = path.lastIndexOf("/");
  const directory = slash >= 0 ? path.slice(0, slash + 1) : "";
  const filename = slash >= 0 ? path.slice(slash + 1) : path;
  const dot = filename.lastIndexOf(".");
  const hasExtension = dot > 0;
  const stem = hasExtension ? filename.slice(0, dot) : filename;
  const extension = hasExtension ? filename.slice(dot) : "";
  const timestamp = date.toISOString().replace(/[-:]/gu, "").replace(/\.\d{3}Z$/u, "Z").replace("T", "-");
  const safeDevice = deviceId.replace(/[^A-Za-z0-9_-]/gu, "-").slice(0, 32);
  return `${directory}${stem}.conflict-${safeDevice}-${timestamp}${extension}`;
}
