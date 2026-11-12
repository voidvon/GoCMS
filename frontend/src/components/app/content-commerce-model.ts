import type { CommerceDimension, CommerceProduct, CommerceSpecification, CommerceVariant } from "@/lib/api"

export const commerceCurrencies = ["USD", "EUR", "GBP", "HKD", "CNY", "JPY", "AUD", "CAD", "CHF", "SGD", "NZD"]
export const maxProductVariants = 500
export const maxProductDimensions = 12
export const maxDimensionOptions = 5000
export type VariantDraft = Omit<CommerceVariant, "amount"> & { price: string; localID: string; fixedPriceInputs: Record<string, string> }
export type CommerceDraft = Omit<CommerceProduct, "variants" | "dimensions"> & { dimensions: CommerceDimension[]; variants: VariantDraft[] }

let sequence = 0
export function newVariant(dimensions: CommerceDimension[] = []): VariantDraft {
  return { id: 0, sku: "", specifications: dimensions.map(d => ({ name: d.name, value: "" })), enabled: true, price: "", fixedPriceInputs: {}, localID: `variant-${++sequence}` }
}

export function commerceDraft(product?: CommerceProduct): CommerceDraft | undefined {
  if (!product) return undefined
  const dimensions = product.dimensions
  return {
    ...product, dimensions,
    variants: product.variants.map(({ amount, ...variant }) => ({
      ...variant,
      specifications: dimensions.map(d => ({ name: d.name, value: variant.specifications?.find(s => s.name.trim().toLowerCase() === d.name.trim().toLowerCase())?.value ?? "" })),
      price: amount > 0 ? String(amount / (product.currency === "JPY" ? 1 : 100)) : "",
      fixedPriceInputs: Object.fromEntries(Object.entries(variant.fixed_prices ?? {}).map(([currency, amount]) => [currency, String(amount / (currency === "JPY" ? 1 : 100))])),
      localID: `variant-${++sequence}`,
    })),
  }
}

function validText(value: string, limit: number) {
  return Boolean(value) && new TextEncoder().encode(value).length <= limit && !/[\0\r\n]/.test(value)
}
export function normalizedDimensions(dimensions: CommerceDimension[]): CommerceDimension[] {
  if (dimensions.length > maxProductDimensions) throw new Error(`每条内容最多 ${maxProductDimensions} 个规格维度`)
  const names = new Set<string>(), keys = new Set<string>()
  return dimensions.map(d => {
    const name = d.name.trim(), unit = d.unit.trim()
    if (!validText(name, 40) || names.has(name.toLowerCase())) throw new Error("规格维度名称不能为空或重复，最多 40 字节")
    if (!validText(d.key, 64) || keys.has(d.key)) throw new Error("规格维度标识无效或重复")
    if (unit && !validText(unit, 20)) throw new Error("规格单位最多 20 字节，不能包含换行")
    if (!["text", "number", "select"].includes(d.type)) throw new Error("请选择有效的规格维度类型")
    names.add(name.toLowerCase()); keys.add(d.key)
    const options = d.type === "select" ? d.options.map(s => s.trim()).filter(Boolean) : []
    if (d.type === "select" && (!options.length || options.length > maxDimensionOptions)) throw new Error(`“${name}”需配置 1 至 ${maxDimensionOptions} 个候选值`)
    if (options.some(s => !validText(s, 120)) || new Set(options).size !== options.length) throw new Error(`“${name}”的候选值不能重复，最多 120 字节`)
    return { ...d, name, unit, options }
  })
}

export function combinationKey(specifications: CommerceSpecification[]) {
  return JSON.stringify(specifications.map(s => [s.name.trim().toLowerCase(), s.value.trim()]).sort(([a], [b]) => a.localeCompare(b)))
}

export function appendCombinations(draft: CommerceDraft): VariantDraft[] {
  const dimensions = normalizedDimensions(draft.dimensions)
  if (!dimensions.length || dimensions.some(d => d.type !== "select")) throw new Error("批量生成需要所有维度均为选项类型，并已配置候选值；文本或数字维度可在表格中逐行填写")
  const count = dimensions.reduce((n, d) => n * d.options.length, 1)
  if (count > maxProductVariants) throw new Error(`候选值共有 ${count} 种组合，超过 ${maxProductVariants} 行上限，请缩减候选值或手动添加需要的组合`)
  // Only an untouched, unsaved placeholder may be replaced. Prices, SKU IDs,
  // currency overrides and incomplete existing rows are never silently removed.
  const variants = draft.variants.filter(v => !v.enabled || v.id || v.sku.trim() || v.price.trim() || Object.values(v.fixedPriceInputs).some(s => s.trim()) || v.specifications.some(s => s.value.trim()))
  const existing = new Set(variants.map(v => combinationKey(v.specifications)))
  let combinations: CommerceSpecification[][] = [[]]
  for (const d of dimensions) combinations = combinations.flatMap(specs => d.options.map(value => [...specs, { name: d.name, value }]))
  const additions = combinations.filter(specs => !existing.has(combinationKey(specs))).map(specifications => ({ ...newVariant(), specifications }))
  if (variants.length + additions.length > maxProductVariants) throw new Error(`补充后将超过 ${maxProductVariants} 行，请先整理现有规格`)
  return [...variants, ...additions]
}

function priceAmount(raw: string, currency: string, label: string) {
  const value = raw.trim(), exponent = currency === "JPY" ? 0 : 2
  if (!(exponent ? /^\d+(\.\d{1,2})?$/ : /^\d+$/).test(value)) throw new Error(`${label}${exponent ? "最多保留两位小数" : "必须为整数"}`)
  const [whole, fraction = ""] = value.split(".")
  const amount = Number(exponent ? whole + fraction.padEnd(2, "0") : whole)
  if (!Number.isSafeInteger(amount) || amount <= 0 || amount > 99999999999) throw new Error(`${label}必须为有效正数`)
  return amount
}

export function commercePayload(draft?: CommerceDraft): CommerceProduct | undefined {
  if (!draft) return undefined
  if ((draft.enabled || draft.variants.some(v => v.price.trim() || Object.values(v.fixedPriceInputs).some(s => s.trim()))) && !commerceCurrencies.includes(draft.currency)) {
    throw new Error("填写售价或开启销售前请选择计价币种")
  }
  const dimensions = normalizedDimensions(draft.dimensions), combinations = new Set<string>(), skus = new Set<string>()
  const variants = draft.variants
    .filter(v => draft.enabled || v.id || v.price.trim() || v.sku.trim() || Object.values(v.fixedPriceInputs).some(s => s.trim()) || v.specifications.some(s => s.value.trim()))
    .map((variant, index) => {
      const label = `第 ${index + 1} 行`
      const specifications = dimensions.map((d, i) => {
        const value = variant.specifications[i]?.value.trim() ?? ""
        if (!validText(value, 120)) throw new Error(`${label}的“${d.name}”需填写有效值，最多 120 字节`)
        if (d.type === "select" && !d.options.includes(value)) throw new Error(`${label}的“${d.name}”不在候选值中，请修改规格或补充候选值`)
        if (d.type === "number" && !/^[+-]?(?:\d+(?:\.\d+)?|\.\d+)$/.test(value)) throw new Error(`${label}的“${d.name}”需要有效数字`)
        return { name: d.name, value, unit: d.unit }
      })
      const key = combinationKey(specifications), sku = variant.sku.trim()
      if (combinations.has(key)) throw new Error(`${label}的规格组合重复，每个组合只能配置一行`)
      if (sku && (!validText(sku, 64) || skus.has(sku.toLowerCase()))) throw new Error(`${label}的规格编号无效或重复，最多 64 字节`)
      combinations.add(key); if (sku) skus.add(sku.toLowerCase())
      const amount = !draft.enabled && !variant.price.trim() ? 0 : priceAmount(variant.price, draft.currency, `${label}售价`)
      const fixed_prices: Record<string, number> = {}
      for (const [currency, raw] of Object.entries(variant.fixedPriceInputs)) {
        if (raw.trim() && currency !== draft.currency) fixed_prices[currency] = priceAmount(raw, currency, `${label} ${currency} 固定售价`)
      }
      return { id: variant.id, sku, specifications, enabled: variant.enabled, amount, fixed_prices }
    })
  if (variants.length > maxProductVariants) throw new Error(`每条内容最多 ${maxProductVariants} 个规格`)
  if (draft.enabled && !variants.some(v => v.enabled)) throw new Error("开启销售后至少需要一个已启用并配置价格的规格")
  return { enabled: draft.enabled, kind: draft.kind, currency: draft.currency, dimensions, variants }
}
