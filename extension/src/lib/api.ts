// Thin fetch() wrappers for the local marko sync HTTP API
// (/health, /plan, /diff, /report). See docs/architecture.md §8.2.
//
// TODO(extension-agent): implement real fetch calls against
// http://127.0.0.1:<port>.

import type { BookmarkTree, Plan } from "./types";

export async function getHealth(_port: number): Promise<{ status: string; markoVersion: string }> {
  throw new Error("not implemented");
}

export async function getPlan(_port: number): Promise<{ generatedAt: string; markoVersion: string; desiredTree: BookmarkTree }> {
  throw new Error("not implemented");
}

export async function postDiff(_port: number, _actualTree: BookmarkTree): Promise<Plan> {
  throw new Error("not implemented");
}

export async function postReport(_port: number, _results: unknown[]): Promise<{ accepted: boolean; okCount: number; errorCount: number }> {
  throw new Error("not implemented");
}
