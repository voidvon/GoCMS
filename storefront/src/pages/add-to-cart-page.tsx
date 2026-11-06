import { localError } from "@/lib/errors"
import { useTranslation } from "react-i18next"
import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { PageHeading } from "@/components/app/commerce-ui"
import { Failure, Loading } from "@/components/app/feedback"
import { getVariants } from "@/lib/api"
import { shoppingReturnPath, navigate } from "@/lib/router"
import { useResource, useShop } from "@/lib/shop-context"

// Static theme pages own product markup and variant selection. This route only
// validates the chosen variant, adds it to the cart, and replaces the handoff URL.
export function AddToCartPage({ search }: { search: string }) {
  const { t } = useTranslation()
  const { add, rememberShoppingURL } = useShop()
  const processed = useRef(false)
  const [saveError, setSaveError] = useState<Error | null>(null)
  const params = new URLSearchParams(search)
  const quantity = params.has("quantity") ? Number(params.get("quantity")) : 1
  const variantID = Number(params.get("variant_id"))
  const contentID = params.has("content_id") ? Number(params.get("content_id")) : null
  const returnURL = shoppingReturnPath(params.get("return_to"))
  params.delete("lang")
  const resource = useResource(`cart:add:${params.toString()}`, async () => {
    if (!Number.isSafeInteger(variantID) || variantID <= 0 || !Number.isInteger(quantity) || quantity < 1 || quantity > 1000 || (contentID !== null && (!Number.isSafeInteger(contentID) || contentID <= 0))) throw localError("errors.invalidCartInput")
    const result = await getVariants([{ variant_id: variantID, quantity }])
    const variant = result.items.find(item => item.id === variantID)
    if (!variant || (contentID !== null && contentID !== variant.content_id)) throw localError("errors.addUnavailable")
    return variant
  })
  useEffect(() => {
    if (!resource.data || processed.current) return
    processed.current = true
    if (!add(variantID, quantity)) { setSaveError(localError("add.saveError")); return }
    rememberShoppingURL(returnURL)
    navigate(params.get("destination") === "checkout" ? `/shop/checkout?currency=${encodeURIComponent(resource.data.currency)}` : "/shop/cart", true)
  }, [resource.data, add, rememberShoppingURL, variantID, quantity, returnURL, search])
  function retry() { processed.current = false; setSaveError(null); resource.reload() }
  return <div className="page mx-auto max-w-xl">
    <PageHeading title={t("add.title")} />
    {resource.error || saveError ? <Failure message={resource.error || saveError} retry={retry} /> : <div className="panel"><Loading label={t("add.checking")} /></div>}
    <Button variant="link" className="mt-5 h-auto min-h-11 whitespace-normal px-0 text-start" nativeButton={false} render={<a href={returnURL} />}>{t("add.return")}</Button>
  </div>
}
