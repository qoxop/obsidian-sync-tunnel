import { describe, expect, it } from "vitest";

import { assertNoPortablePathCollisions, conflictPath, globMatches, pathIsWithin, pathRequiresObsidianRestart } from "../src/path";

describe("pathIsWithin", () => {
  it("matches the protected directory and all descendants", () => {
    expect(pathIsWithin(".obsidian/plugins/sync-tunnel", ".obsidian/plugins/sync-tunnel")).toBe(true);
    expect(pathIsWithin(".obsidian/plugins/sync-tunnel/main.js", ".obsidian/plugins/sync-tunnel")).toBe(true);
    expect(pathIsWithin(".obsidian\\plugins\\sync-tunnel\\data.json", ".obsidian/plugins/sync-tunnel/")).toBe(true);
  });

  it("does not match directories with a shared prefix", () => {
    expect(pathIsWithin(".obsidian/plugins/sync-tunnel-copy/main.js", ".obsidian/plugins/sync-tunnel")).toBe(false);
  });
});

describe("globMatches", () => {
  it("supports recursive and single-segment wildcards", () => {
    expect(globMatches("notes/archive/2026.md", "notes/**")).toBe(true);
    expect(globMatches("notes/2026.md", "notes/*.md")).toBe(true);
    expect(globMatches("notes/archive/2026.md", "notes/*.md")).toBe(false);
  });
});

describe("pathRequiresObsidianRestart", () => {
  it("detects plugin and appearance resources under a custom config directory", () => {
    expect(pathRequiresObsidianRestart(".config/plugins/example/main.js", ".config")).toBe(true);
    expect(pathRequiresObsidianRestart(".config/themes/example/theme.css", ".config")).toBe(true);
    expect(pathRequiresObsidianRestart(".config/snippets/example.css", ".config")).toBe(true);
    expect(pathRequiresObsidianRestart(".config/community-plugins.json", ".config")).toBe(true);
    expect(pathRequiresObsidianRestart("notes/example.md", ".config")).toBe(false);
  });
});

describe("conflictPath", () => {
  it("keeps the extension and sanitizes the device name", () => {
    const date = new Date("2026-08-16T12:34:56.000Z");
    expect(conflictPath("folder/note.md", "my device!", date))
      .toBe("folder/note.conflict-my-device--20260816-123456Z.md");
  });
});

describe("assertNoPortablePathCollisions", () => {
  it("rejects case and Unicode normalization collisions", () => {
    expect(() => assertNoPortablePathCollisions(["Notes/Today.md", "notes/today.md"]))
      .toThrow(/跨平台路径冲突/u);
    expect(() => assertNoPortablePathCollisions(["café.md", "cafe\u0301.md"]))
      .toThrow(/跨平台路径冲突/u);
    expect(() => assertNoPortablePathCollisions(["Notes/Today.md", "notes/tomorrow.md"]))
      .not.toThrow();
  });
});
