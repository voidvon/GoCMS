import i18n from "@/i18n"

// Storefront displays the site's own name without the CMS product brand.
export function storefrontName(name: string) {
  const display = name.replace(/gocms/gi, "").replace(/\s+/g, " ").replace(/^[\s|·:：—–-]+|[\s|·:：—–-]+$/g, "")
  return display || String(i18n.t("design.shoppingLabel"))
}
