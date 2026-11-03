/** What `work` settles to, or `late` if `ms` pass first. The work keeps running either way. */
export async function withDeadline<T, L>(work: Promise<T>, ms: number, late: L): Promise<T | L> {
  let timer: ReturnType<typeof setTimeout> | undefined
  const deadline = new Promise<L>((resolve) => {
    timer = setTimeout(resolve, ms, late)
  })
  try {
    return await Promise.race([work, deadline])
  } finally {
    if (timer !== undefined) clearTimeout(timer)
  }
}
