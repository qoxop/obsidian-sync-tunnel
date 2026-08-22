type RequestHandler = (parameters: Record<string, unknown>) => Promise<unknown>;

let requestHandler: RequestHandler = async () => {
  throw new Error("Unexpected real Obsidian requestUrl call in unit test");
};

export function setRequestUrlHandler(handler: RequestHandler): void {
  requestHandler = handler;
}

export function resetRequestUrlHandler(): void {
  requestHandler = async () => {
    throw new Error("Unexpected real Obsidian requestUrl call in unit test");
  };
}

export async function requestUrl(parameters: Record<string, unknown>): Promise<unknown> {
  return requestHandler(parameters);
}

export class FileSystemAdapter {}

export const Platform = {
  isDesktop: false,
  isMobile: false,
  isDesktopApp: false,
  isMobileApp: false,
  isIosApp: false,
  isAndroidApp: false,
  isMacOS: false,
  isWin: false
};
