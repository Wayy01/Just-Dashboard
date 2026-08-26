"use client"

import { get } from "@/lib/api"
import type { GitHubStatus } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"

/**
 * Who this dashboard is, to GitHub.
 *
 * The path is optional and the two answers are different questions, both
 * worth asking: with a repository it is the account that owns that checkout —
 * the one whose credential a push from it would use — and without one it is
 * the account the dashboard itself runs as, which is what the repository list
 * and the files page can honestly show.
 *
 * Slow on purpose: it shells out to gh, and the answer only changes when
 * somebody signs in — which refreshes it directly rather than waiting.
 */
export function useGitHubAccount(path?: string, enabled = true) {
  return usePoll(
    (signal) => get<GitHubStatus>("/git/github/", path ? { path } : undefined, signal),
    120000,
    [path ?? ""],
    { enabled },
  )
}
