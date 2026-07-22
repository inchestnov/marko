// Thin fetch() wrappers for the local marko sync HTTP API
// (/health, /plan, /diff, /report, /shutdown). See docs/architecture.md §8.2.
//
// The server is a plain HTTP endpoint bound to 127.0.0.1 on a configurable
// port (default 8765, see §8.1). The port is read from chrome.storage.local
// by callers and passed in explicitly here so this module stays a pure
// transport layer with no implicit global state.

import type {
  BookmarkTree,
  DiffResponse,
  ErrorEnvelope,
  HealthResponse,
  OperationResult,
  PlanResponse,
  ReportResponse,
} from "./types";

export const DEFAULT_PORT = 8765;

export const STORAGE_PORT_KEY = "markoPort";

function baseUrl(port: number): string {
  return `http://127.0.0.1:${port}`;
}

/**
 * Error thrown by the api wrappers below. When the server responded with
 * the {error:{code,message}} envelope (§8.2), `code` and `serverMessage`
 * are populated from it; otherwise `code` is undefined and `message` falls
 * back to a generic description of the transport failure.
 */
export class ApiError extends Error {
  code?: string;

  constructor(message: string, code?: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
  }
}

async function request<T>(
  port: number,
  path: string,
  init?: RequestInit
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`${baseUrl(port)}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
    });
  } catch (err) {
    throw new ApiError(
      `Network error contacting marko sync at ${baseUrl(port)}${path}: ${
        err instanceof Error ? err.message : String(err)
      }`
    );
  }

  if (!response.ok) {
    let envelope: ErrorEnvelope | undefined;
    try {
      envelope = (await response.json()) as ErrorEnvelope;
    } catch {
      // Body wasn't JSON / wasn't the expected envelope shape; fall through.
    }
    if (envelope?.error) {
      throw new ApiError(envelope.error.message, envelope.error.code);
    }
    throw new ApiError(`Request to ${path} failed with status ${response.status}`);
  }

  // No-content responses (e.g. /shutdown) may not return a JSON body.
  const text = await response.text();
  if (!text) {
    return undefined as unknown as T;
  }
  return JSON.parse(text) as T;
}

export async function getHealth(port: number): Promise<HealthResponse> {
  return request<HealthResponse>(port, "/health", { method: "GET" });
}

export async function getPlan(port: number): Promise<PlanResponse> {
  return request<PlanResponse>(port, "/plan", { method: "GET" });
}

export async function postDiff(
  port: number,
  actualTree: BookmarkTree
): Promise<DiffResponse> {
  return request<DiffResponse>(port, "/diff", {
    method: "POST",
    body: JSON.stringify({ actualTree }),
  });
}

export async function postReport(
  port: number,
  results: OperationResult[]
): Promise<ReportResponse> {
  return request<ReportResponse>(port, "/report", {
    method: "POST",
    body: JSON.stringify({ results }),
  });
}

/** Optional convenience per §8.2: tells the CLI's one-shot server it can stop. */
export async function postShutdown(port: number): Promise<void> {
  await request<void>(port, "/shutdown", { method: "POST" });
}
