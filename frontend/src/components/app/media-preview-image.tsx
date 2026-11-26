import { ImageOff } from "lucide-react"
import { useState, type ComponentPropsWithoutRef } from "react"
import { contentImagePreviewURL } from "@/lib/api"
import { useLanguage } from "@/lib/language-context"
import { useSite } from "@/lib/site-context"

function useMediaPreviewURL(source: string, languageCode: string | undefined) {
  const { activeSite, activeSiteId } = useSite()
  const { languages, defaultLang } = useLanguage()
  const value = source.trim()
  if (!value) return undefined
  let url: URL
  try {
    url = new URL(value, window.location.origin)
  } catch {
    return undefined
  }

  if (/^([a-z][a-z\d+.-]*:|\/\/)/i.test(value)) {
    if (url.protocol === "http:" || url.protocol === "https:" || url.protocol === "blob:" || /^data:image\//i.test(value)) {
      return url.href
    }
    return undefined
  }
  if (url.origin !== window.location.origin || value.includes("\\")) return undefined

  // These public routes already carry an asset identity and work on the admin host.
  if (url.pathname.startsWith("/assets/") || url.pathname.startsWith("/api/files/media/")) {
    return url.pathname + url.search + url.hash
  }
  if (activeSite?.id !== activeSiteId) return undefined

  const language = languages.find((item) => item.code === (languageCode ?? defaultLang))
  const standalone = language?.is_enabled === 1 && language.is_default !== 1 && language.site_mode === "standalone"
  const origin = activeSite.status === "active" && (standalone ? language.public_origin : activeSite.public_origin)
  if (origin && (window.location.protocol !== "https:" || origin.startsWith("https://"))) {
    return new URL(url.pathname + url.search + url.hash, origin).href
  }
  return contentImagePreviewURL(activeSiteId, url.pathname) + url.hash
}

type MediaPreviewImageProps = Omit<ComponentPropsWithoutRef<"img">, "src" | "onError"> & {
  source: string
  languageCode?: string
}

export function MediaPreviewImage({ source, languageCode, alt, ...props }: MediaPreviewImageProps) {
  const src = useMediaPreviewURL(source, languageCode)
  const [failed, setFailed] = useState<string>()
  if (!src || failed === src) {
    return (
      <span className="inline-flex shrink-0 items-center justify-center text-muted-foreground" title="图片无法加载" role="img" aria-label={alt ? `${alt}，图片无法加载` : "图片无法加载"}>
        <ImageOff className="size-4" aria-hidden="true" />
      </span>
    )
  }
  return <img {...props} key={src} src={src} alt={alt} onError={() => setFailed(src)} />
}
