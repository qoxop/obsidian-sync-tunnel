export async function requestUrl(): Promise<never> {
  throw new Error("Unexpected real Obsidian requestUrl call in unit test");
}
