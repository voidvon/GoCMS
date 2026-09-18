import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react"
import { createPortal } from "react-dom"

type HeaderActionsContextType = {
  container: HTMLDivElement | null
  setContainer: (node: HTMLDivElement | null) => void
}

const HeaderActionsContext = createContext<HeaderActionsContextType | null>(null)

export function HeaderActionsProvider({ children }: { children: ReactNode }) {
  const [container, setContainer] = useState<HTMLDivElement | null>(null)

  const setContainerCallback = useCallback((node: HTMLDivElement | null) => {
    setContainer(node)
  }, [])

  const value = useMemo(
    () => ({ container, setContainer: setContainerCallback }),
    [container, setContainerCallback],
  )

  return (
    <HeaderActionsContext.Provider value={value}>
      {children}
    </HeaderActionsContext.Provider>
  )
}

export function HeaderActionsSlot({ className }: { className?: string }) {
  const context = useContext(HeaderActionsContext)
  return <div ref={context?.setContainer} className={className} />
}

export function HeaderActions({ children }: { children: ReactNode }) {
  const context = useContext(HeaderActionsContext)
  if (!context?.container) return null
  return createPortal(children, context.container)
}
