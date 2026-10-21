import { useCallback, useState, type SetStateAction } from "react"

// Keep selected data available during the exit animation. Clearing the value
// closes the dialog; Base UI's completion callback releases it after unmount.
export function useDialogState<T>() {
  const [state, setState] = useState<{ value: T | null; open: boolean }>({ value: null, open: false })

  const setValue = useCallback((next: SetStateAction<T | null>) => {
    setState((current) => {
      const value = typeof next === "function" ? (next as (previous: T | null) => T | null)(current.value) : next
      return value === null ? { ...current, open: false } : { value, open: true }
    })
  }, [])

  const onOpenChangeComplete = useCallback((open: boolean) => {
    if (!open) {
      // A new selection may have reopened the dialog before an old exit ended.
      setState((current) => current.open ? current : { value: null, open: false })
    }
  }, [])

  return [state.value, setValue, { open: state.open, onOpenChangeComplete }] as const
}
