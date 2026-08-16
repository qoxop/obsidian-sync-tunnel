export async function requestUrl(): Promise<never> {
  throw new Error("Unexpected real Obsidian requestUrl call in unit test");
}

export class FileSystemAdapter {}

export const Platform = {
  isDesktop: false,
  isMobile: false,
  isDesktopApp: false,
  isMobileApp: false,
  isIosApp: false,
  isAndroidApp: false
};
