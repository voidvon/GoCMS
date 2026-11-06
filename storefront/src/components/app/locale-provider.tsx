import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { DirectionProvider } from "@base-ui/react/direction-provider"

export function LocaleProvider({ children }: { children: ReactNode }) {
  const { i18n } = useTranslation()
  return <DirectionProvider direction={i18n.dir()}>{children}</DirectionProvider>
}
