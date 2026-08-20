"use client"

import type { useConfirm } from "@/components/confirm-dialog"

/**
 * What every Docker tab needs from the page above it.
 *
 * The tabs used to live in the page component alongside it — five independent
 * bodies, each with its own poll, dialogs and table, in one 880-line file.
 * They now share the panel primitives with the rest of the product, so the
 * only thing left to hand down is the confirmation opener.
 */

/** The confirm() opener from useConfirm, passed down from the page. */
export type ConfirmFn = ReturnType<typeof useConfirm>["confirm"]
